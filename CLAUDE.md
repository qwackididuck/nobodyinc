# Nobody Inc

Outil d'orchestration d'équipes d'agents Claude Code : équipes, messagerie inter-agents (bannettes normale/urgente), tickets avec livrables, blueprint sans code, supervision web. Vision et liste des travaux : `docs/roadmap.md`. Décisions : `docs/adr/`.

## Stack
- `cmd/nobodyd` : démon Go (Go 1.27), API décrite dans `api/openapi.yaml` (à mettre à jour avant tout changement d'endpoint).
- `internal/` : code Go par domaine.
- `web/` : Next.js 16 (App Router, TypeScript strict), géré avec pnpm.

## Commandes
- `task check` : lint + tests, à passer avant chaque commit.
- `task test` / `task lint` / `task build`
- `task dev:daemon` (écoute sur 127.0.0.1:7420, surchargeable via `NOBODY_ADDR`) et `task dev:web`
- `task web:install`

## Conventions
- Branches : `main` (prod), `develop` (intégration), `<iid>-<description>` pour les features.
- Logs via `log/slog`, jamais de `fmt.Println` permanent.
- Le démon n'écoute que sur loopback par défaut.
- Données structurées sur les agents : hooks et OTel uniquement, jamais de lecture de la sortie du PTY.
