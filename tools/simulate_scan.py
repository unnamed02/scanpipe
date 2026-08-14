"""模拟 NWBox 扫描客户端的完整行为（从 scanner.pcapng 逆向的协议）。

用法:
    python simulate_scan.py [ingest_addr] [批次uuid]

依赖: requests、websocket-client（pip install requests websocket-client）
"""
import base64
import hashlib
import json
import sys
import uuid as uuidlib

import requests
import websocket  # websocket-client


def simulate_batch(ingest: str, pages: list[str], client_id: str, batch_uuid: str):
    ws = websocket.create_connection(f"{ingest.replace('http', 'ws')}/ws?client_id={client_id}")
    success = 0
    for i, img_path in enumerate(pages, 1):
        raw = open(img_path, "rb").read()
        body = {
            "client_id": client_id,
            "uuid": batch_uuid,
            "item_id": 1,
            "paper_number": 1,
            "page_number": i,
            "front": i % 2 == 1,
            "file_type": "jpg",
            "file_size": len(raw),
            "check_sum": hashlib.md5(raw).hexdigest(),
            "data": base64.b64encode(raw).decode(),
        }
        resp = requests.post(f"{ingest}/api/upload", json=body,
                             headers={"User-Agent": "NWBox"}, timeout=60).json()
        print(f"  page {i}: {resp['message']}")
        ok = bool(resp.get("success"))
        success += ok
        ws.send(json.dumps({
            "type": "upload_status",
            "payload": {"uuid": batch_uuid, "paper_number": 1,
                        "page_number": i, "success": ok,
                        "path": resp.get("path", "")},
        }))
    ws.send(json.dumps({"type": "scan_status",
                        "payload": {"uuid": batch_uuid, "status": "finished"}}))
    ws.send(json.dumps({"type": "upload_finish",
                        "payload": {"uuid": batch_uuid, "total_pages": len(pages),
                                    "success_count": success,
                                    "fail_count": len(pages) - success}}))
    ws.close()
    print(f"batch {batch_uuid}: {success}/{len(pages)} uploaded")


if __name__ == "__main__":
    ingest = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:5665"
    batch = sys.argv[2] if len(sys.argv) > 2 else str(uuidlib.uuid4())
    pages = sys.argv[3:] or ["../page_1.jpg", "../page_2.jpg"]
    simulate_batch(ingest, pages, "24B72A466975", batch)
