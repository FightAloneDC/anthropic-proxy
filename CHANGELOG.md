# Changelog

All notable changes to this project will be documented in this file.

## [2.2.2] - 2026-06-19

### Added
- Thinking tag normalization for all proxy modes (Anthropic, OpenAI Responses, Gemini, ChatCompletions)
- Stateful streaming parser for thinking tags split across chunks
- Support for both `<think>` and `<thinking>` tag formats
- Mismatched closing tag handling (`</think>` and `</thinking>`)
- Incomplete opening tag buffering for streaming
- Standalone closing tag removal
- Duplicate reasoning prevention when backend sends both `reasoning` field and thinking tags in content

### Fixed
- Thinking tags leaking into content field when backend sends split tags across chunks
- Duplicate reasoning emission when backend sends same content in both `reasoning` and `content` fields
- Mismatched opening/closing tags not being handled correctly

### Changed
- Stream normalizer now buffers incomplete opening tags until they're complete
- Stream normalizer looks for both `</think>` and `</thinking>` closing tags
- Non-streaming normalization uses regex for complete tag extraction
