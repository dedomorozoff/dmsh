# План реализации `dmsh`

## Цель
Собрать Linux-first shell на Go, где пользователь пишет на человеческом языке, а локальная LLM через `llama.cpp` (CGO, без HTTP) предлагает и безопасно запускает команды.

## Статус задач
- `scaffold-cli` — completed
- `cgo-build` — completed
- `llama-engine` — completed
- `json-contract` — completed
- `safety-gate` — completed
- `repl-ux` — completed
- `tests-hardening` — completed

## Реализованные блоки
- CLI: `ask`, `run`, `repl`, `version`.
- Конфиг: загрузка из `~/.config/dmsh/config.json` + флаги запуска.
- Контракт LLM: строгий JSON + retry repair при невалидном ответе.
- Safety policy: deny/suspicious правила, confirm flow, `dry-run` по умолчанию.
- Исполнитель: запуск shell-команд через `exec.CommandContext`.
- LLM engine:
  - stub без CGO для локальной разработки,
  - реальный `llama.cpp` движок под build tag `llama`.
- Сборка: `Makefile`, `llama.cpp` как submodule.
- Тесты: парсинг контракта и policy rules.

## Следующий фокус
- Контекстный слой (cwd + snapshot + budget).
- Улучшение chat template под разные семейства моделей.
- Улучшение REPL UX (стриминг, форматирование).
