# Nobody Inc — roadmap (fichier local, non versionné)

Mise à jour : 2026-11-11.

## Contexte

Les outils existants ne couvrent chacun qu'une partie du besoin (Paperclip séquentiel et lent, OpenRig trop orienté terminal, munder-difflin sans équipes, oh-my-claudecode dépendant du flag Agent Teams, Hermes qui ne pilote pas Claude Code). Nobody Inc reprend les équipes façon rig, le protocole de messages du hive, une session god, des tickets traçables, un blueprint sans code et une UI de supervision. Le démon tourne en local ou sur un VPS.

## Décisions

| Sujet | Décision |
|---|---|
| Utilisateurs | Solo. Démon Go en local ou sur VPS, UI web Next.js. |
| Agents | La CLI `claude` dans un PTY. Données structurées par les hooks et OTel, jamais par lecture de l'écran ; on n'écrit dans le PTY que des frappes. |
| Isolation | Agents isolés des réglages globaux de l'utilisateur et de l'environnement d'une session parente (ADR-0003). |
| Concepts | Messages (bannettes normale et urgente) et tickets (unités de travail) ; les messages se rattachent à un ticket. |
| Équipes | **Légères** : périmètre de droits et de channels, contexte commun, adresse collective, file de tickets. Parallélisme = plusieurs instances d'un agent qui piochent un ticket chacune. Challenge = rôles distincts ou modèles différents (le débat entre modèles différents reste utile). |
| Git | **Un worktree et une branche par ticket** (et non plus par équipe). Phase finale d'intégration. |
| Livraison du courrier | `UserPromptSubmit` au réveil (prompt + courrier), `PostToolUse` pour l'urgent en cours de tour, `Stop` pour le courrier arrivé pendant le tour. |
| Message urgent | `query` urgente à un agent occupé : réponse par embranchement de session sans outils, note `nobody` à l'agent ; `ESCALATE` → retour en bannette. |
| Anti-boucle | Pas d'accusé en réponse à un message qui n'attendait pas de réponse ; plafond de 10 échanges par fil. |
| Validations humaines | Permissions (fait), puis nœuds du blueprint, escalades de god, budgets. |
| Commits | L'utilisateur commite et pousse lui-même. Docs versionnées en anglais. |
| UI | Simple pour l'instant ; une passe de design dédiée sera faite ensemble. |

## Fait

- **Fondations** : dépôt, Taskfile, Go + Next.js 16, OpenAPI, golangci-lint, ADR-0001 à 0003, docs (`architecture.md`, `hooks.md`, `approvals.md`).
- **Organisation** : `nobody.yaml` (équipes, agents `agent@équipe`, modèle par agent, channels à sens unique), adresses `human` et `nobody`.
- **Messagerie** : format hive (acts, fils, `in_reply_to`, hops), règles d'envoi, anti-boucle, SQLite, refus explicites pour l'agent.
- **Livraison** : `nobody-hook` (relais Go), hooks `UserPromptSubmit` / `PostToolUse` / `Stop`, livraison « au moins une fois », statut des agents, réveil des agents inactifs, tour d'accueil.
- **MCP interne** : `send_message`, `list_contacts`.
- **Runtime** : `claude` dans un PTY par agent, terminal en WebSocket, environnement nettoyé, démarrage/redémarrage par API.
- **Questions urgentes** par embranchement de session.
- **File d'approbations** : hook `PermissionRequest`, attente de la décision humaine, repli sur le terminal.
- **UI** : équipes et statuts, terminal xterm.js, fil des messages, envoi en tant qu'humain, file d'approbations.
- **Tests** : unitaires, contrats contre la vraie CLI (`task contract`), bout en bout gratuits avec `test/fakeclaude` (`test/e2e`).

## Reste à faire

### Tickets et travail
- Fait : tickets (statuts, équipe, assigné, dépendances, AC), livrables, commentaires, messages rattachés, outils MCP `ticket_*` et `deliverable_add`, API et onglet Tickets, notifications `nobody`, migrations SQLite.
- Reste : timeline unifiée par ticket (événements, coûts, diffs) ; appels d'offres (`bid`).

### Équipes légères
- Fait : file de tickets par équipe avec notification d'un membre disponible ; adressage d'équipe `@dev`.
- Reste : instances multiples d'un même agent, une par ticket (avec worktree par ticket).
- Rôles distincts (dev / reviewer), lead facultatif ; équipes par défaut (specs, dev, validation, pentest, métro, intégration).

### Messagerie
- `broadcast` (organisation ou équipe) ; plafond d'échanges compté depuis le dernier message humain ; fils et bannettes par agent dans l'UI ; outils `board_read` / `board_propose`.

### Session god
- Chat de recueil du besoin (doc via ragflow, GitLab) → besoin + AC validés ; délégation (création, découpage, attribution de tickets) ; appels d'offres ; arbitrage et escalade.

### Catalogue d'agents
- Format markdown + en-tête (compatible subagents) ; bundle de modèles prêts à l'emploi ; recruter / personnaliser / dupliquer / créer ; import local et en ligne avec relecture de sécurité.

### Blueprint et moteur d'exécution
- Éditeur visuel (god, équipes, validations humaines, flux, channels) ; validation ; export/import YAML.
- Runs : instanciation, machine à états des phases, validations entre phases, intégration finale, pause/reprise/arrêt.

### Git et espaces de travail
- Clonage du projet sur la machine du démon ; worktree + branche par ticket ; réservations de fichiers ; plus tard MR GitLab.

### MCP des agents
- Registre, santé, affectation par agent/équipe/Run, restrictions par outil, lancement en `--strict-mcp-config`, installation depuis l'UI, `request_mcp` via la file d'approbations, relecture de sécurité, plafonds de dépense par outil.

### Sécurité
- Équipe validation sans accès au code en phase 1 ; approbations étendues (blueprint, escalades, budgets) ; coffre de secrets et masquage ; authentification de l'API et accès distant pour le VPS.

### Budgets et observabilité
- Coûts et tokens par agent/équipe/Run/ticket ; budgets avec pause ; plafonds (agents simultanés, durée de tour) ; OTel avec identifiants ; Langfuse ou vue intégrée ; vue « raisonnement » (JSONL).

### Runtime
- Adaptateur de runtime (Codex plus tard) ; redémarrage auto après crash ; reprise des sessions après redémarrage du démon ; snapshot/restauration d'un Run.

### Base de connaissances
- Tableau partagé du Run ; agent documentaliste ; option ragflow.

### UI (passe de design dédiée prévue)
- Tableau de bord des Runs, kanban des tickets, éditeurs (agents, équipes, blueprint), diffs Monaco, coûts, MCP, paramètres, temps réel (WebSocket/SSE).

### Fondations et qualité
- Validation par schéma JSON généré depuis l'OpenAPI ; CI ; tests Playwright de l'UI.

### Déploiement
- Binaire unique embarquant l'UI ; mode VPS (systemd/Docker, auth Claude Code, sauvegardes SQLite).

### Documentation
- Guide utilisateur.

### Plus tard
- Autres runtimes (Codex, Gemini CLI), intégration GitLab poussée, multi-utilisateurs.

### À vérifier sur l'existant
- L'UI dans un navigateur (dont le panneau d'approbations) ; les questions urgentes avec de vrais agents.

## Décisions ouvertes
- Granularité des channels : par équipe seulement, ou aussi par agent.
- Front en production : export statique servi par Go, ou serveur Next.js séparé.
- Bibliothèque du canvas de blueprint.
- Stockage des traces : Langfuse ou vue intégrée.
- Stockage des livrables : dans le dépôt du projet ou dans l'outil.
