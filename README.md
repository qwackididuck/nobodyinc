# Nobody Inc

Faire travailler des équipes d'agents Claude Code (specs, dev, validation, pentest, métrologie, documentation) qui communiquent entre elles, avec tickets traçables, validations humaines et supervision web.

> Projet en démarrage — voir [`docs/roadmap.md`](docs/roadmap.md) et [`docs/adr/`](docs/adr/).

## Prérequis
Go 1.27+, Node 22+, pnpm, [Task](https://taskfile.dev), golangci-lint v2, Claude Code.

## Démarrer
```bash
task web:install
task dev:daemon   # http://127.0.0.1:7420/healthz
task dev:web      # http://localhost:3000
task check        # lint + tests
```
