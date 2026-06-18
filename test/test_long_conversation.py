#!/usr/bin/env python3
"""Test thinking tag normalization in a long conversation."""

import requests
import json
import time

PROXY_URL = "http://localhost:8006/openai/v1/chat/completions"

PROMPTS = [
    "hi",
    "anda tahu film MacGyver?",
    "siapa pemeran aslinya?",
    "kenapa dia terkenal banyak akal?",
    "ceritakan tentang teknik improvisasi yang dia gunakan",
    "bagaimana jika teknik itu diterapkan di dunia nyata?",
    "apa contoh kasus nyata yang mirip dengan cara kerja MacGyver?",
    "menurutmu apakah AI bisa melakukan hal yang sama?",
    "bagaimana cara AI menyelesaikan masalah dengan keterbatasan resource?",
    "berikan contoh implementasi konkret dalam pemrograman",
]


def test_long_conversation(model: str):
    """Test with accumulating conversation context."""
    print(f"\n{'='*60}")
    print(f"Model: {model}")
    print(f"{'='*60}")

    messages = []
    issues = []

    for i, prompt in enumerate(PROMPTS):
        print(f"\n--- Turn {i+1}: {prompt[:50]}... ---")
        messages.append({"role": "user", "content": prompt})

        payload = {
            "model": model,
            "messages": messages,
            "stream": True,
        }

        reasoning_chunks = []
        content_chunks = []
        has_thinking_tags = False

        try:
            resp = requests.post(PROXY_URL, json=payload, stream=True, timeout=120)
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
                    if "choices" in chunk and len(chunk["choices"]) > 0:
                        delta = chunk["choices"][0].get("delta", {})
                        reasoning = delta.get("reasoning", "")
                        content = delta.get("content", "")

                        if reasoning:
                            reasoning_chunks.append(reasoning)
                        if content:
                            content_chunks.append(content)
                            if "<think>" in content or "<thinking>" in content:
                                has_thinking_tags = True
                except json.JSONDecodeError:
                    pass

            full_content = "".join(content_chunks)

            # Check for issues
            if has_thinking_tags:
                issues.append(f"Turn {i+1}: Thinking tags in content")
                print(f"❌ FAIL: Thinking tags in content")
            else:
                print(f"✅ OK: Clean content")

            # Add assistant response to messages for next turn
            messages.append({"role": "assistant", "content": full_content})

            # Print preview
            if reasoning_chunks:
                print(f"Reasoning: {''.join(reasoning_chunks)[:100]}...")
            print(f"Content: {full_content[:100]}...")

        except Exception as e:
            print(f"❌ ERROR: {e}")
            issues.append(f"Turn {i+1}: {e}")

        time.sleep(1)

    print(f"\n{'='*60}")
    print(f"SUMMARY for {model}")
    print(f"{'='*60}")
    print(f"Total turns: {len(PROMPTS)}")
    print(f"Issues: {len(issues)}")

    if issues:
        for issue in issues:
            print(f"  - {issue}")
    else:
        print("✅ All turns passed!")

    return len(issues)


def main():
    """Run long conversation test."""
    print("Testing thinking tag normalization in long conversations")
    print(f"Proxy: {PROXY_URL}")

    models = [
        "kr/claude-sonnet-4.5-thinking-agentic",
        "tokenrouter/MiniMax-M3",
    ]

    total_issues = 0
    for model in models:
        issues = test_long_conversation(model)
        total_issues += issues

    print(f"\n\n{'='*60}")
    print(f"FINAL RESULT")
    print(f"{'='*60}")
    print(f"Total issues: {total_issues}")

    return total_issues


if __name__ == "__main__":
    exit(main())
