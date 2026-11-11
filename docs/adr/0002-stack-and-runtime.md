# ADR-0002 — Stack : démon Go, UI Next.js, SQLite, CLI Claude Code dans un PTY

- Statut : accepté
- Date : 2026-11-11

## Contexte

L'outil doit lancer et superviser de nombreuses sessions d'agents, router des messages, recevoir des hooks, exposer un serveur MCP et tourner aussi bien en local que 24h/24 sur un VPS, avec une interface de supervision consultable à distance.

## Décision

- **Démon Go** (`cmd/nobodyd`) : un seul binaire, beaucoup de PTY et de flux en parallèle grâce aux goroutines, faible empreinte mémoire. Un navigateur ne peut ni lancer de processus, ni accéder à git, ni rester actif une fois fermé : un backend permanent est donc obligatoire.
- **UI Next.js** (`web/`, App Router, TypeScript strict), servie par le démon, unique client pour les modes local et VPS.
- **SQLite** comme source de vérité (état, messages, tickets, journal d'événements) ; les décisions et livrables sont exportés en markdown versionné.
- **CLI `claude` lancée dans un PTY** plutôt que l'Agent SDK : réutilise tels quels les settings, hooks, skills et MCP de l'utilisateur, et permet à l'humain de reprendre la main dans le terminal d'un agent. Les données structurées proviennent des hooks et d'OTel, jamais de la lecture de l'écran ; la lecture des transcripts JSONL (format interne non documenté) est optionnelle et isolée.
- Le démon écoute par défaut sur `127.0.0.1` : il lance des processus et ne doit pas être exposé sans authentification.

## Conséquences

- Deux langages (Go, TypeScript), alignés sur la stack habituelle.
- Mode d'intégration du front (export statique servi par Go ou serveur Next.js séparé) encore à trancher.
- Un faux runtime `claude` sera nécessaire pour tester sans coût d'API.
