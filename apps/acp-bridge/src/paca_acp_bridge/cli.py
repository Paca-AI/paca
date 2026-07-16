"""CLI entry point: `paca-acp-bridge run --agent-id <id> --token <token> --server <url>`."""

from __future__ import annotations

import argparse
import asyncio
import logging
import os
import sys

from .bridge_client import BridgeClient


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="paca-acp-bridge")
    sub = parser.add_subparsers(dest="command", required=True)

    run = sub.add_parser(
        "run",
        help="Connect to Paca and serve ACP conversations from the current directory",
    )
    run.add_argument(
        "--agent-id", default=os.environ.get("PACA_ACP_AGENT_ID"),
        help="The ACP agent's id (or PACA_ACP_AGENT_ID)",
    )
    run.add_argument(
        "--token", default=os.environ.get("PACA_ACP_TOKEN"),
        help="The agent's local-bridge token, generated in Paca's UI (or PACA_ACP_TOKEN)",
    )
    run.add_argument(
        "--server", default=os.environ.get("PACA_ACP_SERVER"),
        help="Your Paca instance's base URL, e.g. https://paca.example.com (or PACA_ACP_SERVER)",
    )
    run.add_argument(
        "--workspace", default=os.getcwd(),
        help="Directory the ACP server operates on (default: current directory)",
    )
    run.add_argument("--log-level", default="INFO")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _build_parser().parse_args(argv)
    logging.basicConfig(
        level=args.log_level.upper(),
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )

    if not args.agent_id or not args.token:
        print(
            "error: --agent-id and --token are required "
            "(or PACA_ACP_AGENT_ID / PACA_ACP_TOKEN)",
            file=sys.stderr,
        )
        return 2
    if not args.server:
        print(
            "error: --server is required (or PACA_ACP_SERVER) — "
            "e.g. https://your-paca-instance.example.com",
            file=sys.stderr,
        )
        return 2

    client = BridgeClient(
        server=args.server,
        agent_id=args.agent_id,
        token=args.token,
        workspace=args.workspace,
    )
    try:
        asyncio.run(client.run_forever())
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
