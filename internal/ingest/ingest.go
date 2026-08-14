// Package ingest 实现 NWBox 扫描客户端协议的替代服务端。
//
// 协议（从 scanner.pcapng 逆向）：
//
//	POST /api/upload   每页一次，JSON：client_id/uuid/item_id/paper_number/
//	                   page_number/front/file_type/file_size/check_sum(MD5)/data(base64 JPEG)
//	                   响应：{"success":true,"message":"image uploaded successfully",
//	                         "path":"uploads\\<client_id>\\<uuid>_<item>_<page>.jpg"}
//	GET  /ws?client_id=X  WebSocket：客户端上报 upload_status/scan_status/
//	                      upload_finish，服务端只收不回
//
// 收到的页校验 MD5 后 MQADD 到 raw_pages 流，进入分类-批改管线。
package ingest

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"scanpipe/internal/mqclient"
)

// Server 是 ingest HTTP/WS 服务。
type Server struct {
	MQ        *mqclient.Client
	SrcStream string // 目标流，默认 raw_pages

	httpSrv *http.Server
}

type uploadRequest struct {
	ClientID    string `json:"client_id"`
	UUID        string `json:"uuid"`
	ItemID      int    `json:"item_id"`
	PaperNumber int    `json:"paper_number"`
	PageNumber  int    `json:"page_number"`
	Front       bool   `json:"front"`
	FileType    string `json:"file_type"`
	FileSize    int    `json:"file_size"`
	CheckSum    string `json:"check_sum"`
	Data        string `json:"data"`
}

// pageMessage 是 raw_pages 流的消息结构（PROTOCOL.md）。
// 与 uploadRequest 的字段重命名：data→image、check_sum→md5。
type pageMessage struct {
	Type        string `json:"type"`
	Schema      int    `json:"schema"`
	ClientID    string `json:"client_id"`
	UUID        string `json:"uuid"`
	ItemID      int    `json:"item_id"`
	PaperNumber int    `json:"paper_number"`
	PageNumber  int    `json:"page_number"`
	Front       bool   `json:"front"`
	FileType    string `json:"file_type"`
	FileSize    int    `json:"file_size"`
	MD5         string `json:"md5"`
	Image       string `json:"image"`
}

// message 把上传请求转成队列消息；base64 原样透传不重编码。
func (req *uploadRequest) message() pageMessage {
	return pageMessage{
		Type: "scan_page", Schema: 1,
		ClientID: req.ClientID, UUID: req.UUID, ItemID: req.ItemID,
		PaperNumber: req.PaperNumber, PageNumber: req.PageNumber,
		Front: req.Front, FileType: req.FileType, FileSize: req.FileSize,
		MD5: req.CheckSum, Image: req.Data,
	}
}

func (s *Server) stream() string {
	if s.SrcStream == "" {
		return "raw_pages"
	}
	return s.SrcStream
}

// ListenAndServe 启动 HTTP 服务，阻塞至出错或 Shutdown 被调用。
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/ws", s.handleWS)
	s.httpSrv = &http.Server{Addr: addr, Handler: mux}
	log.Printf("ingest listening on %s (POST /api/upload, GET /ws)", addr)
	err := s.httpSrv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown 优雅停止（等待在途请求完成，上限 10s）。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.httpSrv.Shutdown(c)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req uploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "bad json: "+err.Error())
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeError(w, "bad base64: "+err.Error())
		return
	}
	sum := md5.Sum(raw)
	if hex.EncodeToString(sum[:]) != req.CheckSum {
		writeError(w, "checksum mismatch")
		return
	}

	payload, _ := json.Marshal(req.message())
	if _, err := s.MQ.Add(s.stream(), string(payload)); err != nil {
		writeError(w, "mq append failed: "+err.Error())
		return
	}
	// 响应格式严格照抄原服务端（含 Windows 反斜杠，客户端可能解析）
	path := fmt.Sprintf("uploads\\\\%s\\\\%s_%d_%d.jpg",
		req.ClientID, req.UUID, req.ItemID, req.PageNumber)
	writeOK(w, path)
}

func writeError(w http.ResponseWriter, message string) {
	json.NewEncoder(w).Encode(map[string]any{"success": false, "message": message})
}

func writeOK(w http.ResponseWriter, path string) {
	json.NewEncoder(w).Encode(map[string]any{
		"success": true, "message": "image uploaded successfully", "path": path,
	})
}

// handleWS 接收扫描客户端的状态汇报。只收不回（与原服务端行为一致），
// upload_finish 留作将来"整卷到齐触发"的钩子。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // 内网客户端不带 Origin
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return // 客户端断开即结束
		}
		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		log.Printf("ingest ws [%s] %s: %s", clientID, msg.Type, string(msg.Payload))
		// 整卷扫完 → 发批次事件，驱动下游整卷触发（协议见 PROTOCOL.md batch_events）
		if msg.Type == "upload_finish" {
			event, _ := json.Marshal(map[string]any{
				"type":      "upload_finish",
				"client_id": clientID,
				"payload":   msg.Payload,
			})
			if _, err := s.MQ.Add("batch_events", string(event)); err != nil {
				log.Printf("ingest ws [%s] batch event mqadd failed: %v", clientID, err)
			}
		}
	}
}
