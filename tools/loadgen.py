"""scanmq 压测数据生成器：往指定流灌入大量记录，测吞吐。

用法:
    python loadgen.py <stream> <count> [--size 200] [--producers 8] [--addr 127.0.0.1:6380]
    python loadgen.py paper_00139_p6 100000 --size 60 --producers 16

默认生成符合当前契约的轻消息（copy_number/key 随机化），
--raw 则生成固定字符串 payload。
"""
import argparse
import json
import random
import threading
import time

import redis


def make_payload(i: int, size: int, raw: bool) -> str:
    if raw:
        return "x" * size
    msg = {"copy_number": random.randint(1, 60),
           "key": f"paper/00139/{random.randint(1, 60)}/page_{random.randint(1, 8)}.jpg"}
    s = json.dumps(msg, separators=(",", ":"))
    return s + " " * max(0, size - len(s))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("stream")
    ap.add_argument("count", type=int)
    ap.add_argument("--size", type=int, default=200, help="payload 目标字节数")
    ap.add_argument("--producers", type=int, default=8)
    ap.add_argument("--addr", default="127.0.0.1:6379")
    ap.add_argument("--password", default=None)
    ap.add_argument("--raw", action="store_true")
    args = ap.parse_args()

    host, port = args.addr.split(":")
    per = args.count // args.producers
    counters = [0] * args.producers

    def worker(idx: int):
        r = redis.Redis(host=host, port=int(port), password=args.password,
                        decode_responses=True)
        pipe = r.pipeline(transaction=False)
        for j in range(per):
            pipe.xadd(args.stream, {"p": make_payload(idx * per + j, args.size, args.raw)})
            if (j + 1) % 500 == 0:
                pipe.execute()
                counters[idx] = j + 1
        pipe.execute()
        counters[idx] = per

    t0 = time.time()
    threads = [threading.Thread(target=worker, args=(i,)) for i in range(args.producers)]
    for t in threads:
        t.start()
    # 进度
    while any(t.is_alive() for t in threads):
        done = sum(counters)
        dt = time.time() - t0
        print(f"\r{done}/{args.count}  {done/max(dt,0.01):.0f} msg/s", end="", flush=True)
        time.sleep(0.5)
    for t in threads:
        t.join()
    dt = time.time() - t0
    print(f"\n完成: {args.count} 条 / {dt:.2f}s = {args.count/dt:.0f} msg/s "
          f"(payload~{args.size}B, {args.producers} 生产者)")


if __name__ == "__main__":
    main()
