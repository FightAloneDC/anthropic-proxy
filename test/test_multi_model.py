#!/usr/bin/env python3
"""Test thinking tag normalization across multiple models in one session."""

import requests
import json
import time
import os

PROXY_URL = "http://localhost:8006/openai/v1/chat/completions"
LOG_DIR = os.path.join(os.path.dirname(__file__), "..", "logs")
os.makedirs(LOG_DIR, exist_ok=True)

MODELS = [
    "kr/claude-haiku-4.5-thinking",
    "kr/claude-haiku-4.5-agentic",
    "kr/claude-haiku-4.5-thinking-agentic",
]

PROMPTS = [
    "anda tahu film MacGyver?",
    "siapa pemeran aslinya?",
    "kenapa dia terkenal banyak akal?",
]


def test_streaming(model: str, prompt: str, session_id: int) -> dict:
    """Test streaming and capture output."""
    print(f"\n{'='*60}")
    print(f"Model: {model}")
    print(f"Prompt: {prompt}")
    print(f"{'='*60}")

    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "stream": True,
    }

    result = {
        "model": model,
        "prompt": prompt,
        "reasoning_chunks": [],
        "content_chunks": [],
        "raw_chunks": [],
        "has_thinking_tags_in_content": False,
        "has_duplicate_reasoning": False,
    }

    try:
        resp = requests.post(PROXY_URL, json=payload, stream=True, timeout=60)
        resp.raise_for_status()

        for line in resp.iter_lines():
            if not line:
                continue
            line = line.decode("utf-8")
            if not line.startswith("data: "):
                continue
            data = line[6:]
            if data == "[DONE]":
                break

            try:
                chunk = json.loads(data)
                result["raw_chunks"].append(chunk)

                if "choices" in chunk and len(chunk["choices"]) > 0:
                    delta = chunk["choices"][0].get("delta", {})
                    reasoning = delta.get("reasoning", "")
                    content = delta.get("content", "")

                    if reasoning:
                        result["reasoning_chunks"].append(reasoning)
                    if content:
                        result["content_chunks"].append(content)
                        # Check for thinking tags in content
                        if "<think>" in content or "<thinking>" in content:
                            result["has_thinking_tags_in_content"] = True
            except json.JSONDecodeError:
                pass

        # Check for duplicate reasoning
        full_reasoning = "".join(result["reasoning_chunks"])
        full_content = "".join(result["content_chunks"])

        if full_reasoning and full_content:
            # Check if content starts with reasoning (duplicate)
            if full_content.startswith(full_reasoning) or full_reasoning in full_content:
                result["has_duplicate_reasoning"] = True

        # Print summary
        print(f"\nReasoning chunks: {len(result['reasoning_chunks'])}")
        print(f"Content chunks: {len(result['content_chunks'])}")
        print(f"Has thinking tags in content: {result['has_thinking_tags_in_content']}")
        print(f"Has duplicate reasoning: {result['has_duplicate_reasoning']}")

        if result["reasoning_chunks"]:
            print(f"\nReasoning preview: {full_reasoning[:200]}...")
        if result["content_chunks"]:
            print(f"\nContent preview: {full_content[:200]}...")

    except Exception as e:
        print(f"Error: {e}")
        result["error"] = str(e)

    return result


def main():
    """Run tests across multiple models in one session."""
    print("Testing thinking tag normalization")
    print(f"Proxy: {PROXY_URL}")
    print(f"Models: {MODELS}")

    all_results = []
    session_id = int(time.time())

    for model in MODELS:
        for prompt in PROMPTS:
            result = test_streaming(model, prompt, session_id)
            all_results.append(result)
            time.sleep(1)  # Small delay between requests

    # Save results to log file
    log_file = os.path.join(LOG_DIR, f"multi-model-test-{session_id}.json")
    with open(log_file, "w") as f:
        json.dump(all_results, f, indent=2, ensure_ascii=False)

    print(f"\n\n{'='*60}")
    print("SUMMARY")
    print(f"{'='*60}")

    issues_found = 0
    for r in all_results:
        status = "OK"
        if r.get("has_thinking_tags_in_content"):
            status = "FAIL - thinking tags in content"
            issues_found += 1
        if r.get("has_duplicate_reasoning"):
            status = "FAIL - duplicate reasoning"
            issues_found += 1
        if r.get("error"):
            status = f"ERROR - {r['error']}"
            issues_found += 1

        print(f"{r['model']}: {status}")

    print(f"\nIssues found: {issues_found}")
    print(f"Log saved to: {log_file}")

    return issues_found


if __name__ == "__main__":
    exit(main())
