# Outil d'équipes d'agents : liste complète des travaux

## Contexte

Les outils existants ne couvrent chacun qu'une partie du besoin :
- Paperclip est séquentiel et lent ;
- OpenRig est trop orienté terminal ;
- munder-difflin a trop d'habillage « The Office » et pas de notion d'équipe ;
- oh-my-claudecode dépend du flag expérimental Agent Teams.

Objectif : construire un outil qui reprend
- les équipes d'agents d'OpenRig (« façon rig ») ;
- le protocole de messages du hive de munder-difflin (bannettes normale et urgente) ;
- une session **god** qui recueille le besoin, délègue ou lance des appels d'offres ;
- des **tickets** traçables, avec livrables et commentaires ;
- un **éditeur de blueprint** sans code (équipes, workflow, channels unidirectionnels) ;
- une **UI web** pour tout superviser.

Le backend doit tourner en local (il s'arrête avec la machine) ou sur un VPS (24h/24), pour tout type de projet.

## Décisions déjà prises

| Sujet | Décision |
|---|---|
| Utilisateurs | Solo. Le backend tourne en local ou sur un VPS, le client est une app web Next.js servie par le backend Go. |
| Backend | Un démon Go est nécessaire, car un navigateur ne peut ni lancer de processus, ni accéder aux fichiers et à git, ni rester actif quand on le ferme. Le démon fait tourner les sessions `claude`, route les messages, reçoit les hooks, expose le serveur MCP, applique les budgets, persiste l'état, et tourne 24h/24 sur VPS sans navigateur ouvert. Go plutôt que Node : un seul binaire, beaucoup de PTY et de flux en parallèle grâce aux goroutines, peu de mémoire, ta stack. |
| Runtimes | Claude Code d'abord, derrière une interface d'adaptateur extensible (Codex plus tard). |
| Agents | La CLI `claude` lancée dans un PTY (terminal virtuel), avec tes settings, hooks et skills. Les données structurées viennent des hooks et d'OTel, jamais de la lecture de l'écran. |
| Concepts | Les **messages** (conversations, bannettes normale et urgente) et les **tickets** (unités de travail suivies) coexistent, les messages se rattachent à un ticket. |
| Catalogue d'agents | Fichiers markdown avec en-tête YAML, compatibles avec les subagents Claude Code, plus des champs propres à l'outil. Agents dupliquables, modifiables, créables. |
| Workflow | Configurable par blueprint sans code : des équipes reliées par des flux de workflow, et des **channels unidirectionnels** (A→B : A peut solliciter B, B répond mais ne peut pas solliciter A). Un chemin non déclaré bloque la communication. |
| Expression du besoin | Dans l'outil, par un chat avec god, qui produit le besoin et les AC validés par l'humain. |
| Message urgent | Selon le type. Une `query` reçoit une réponse immédiate par embranchement de session, sans outils, avec une copie dans la bannette du destinataire. Une `request` ou une alerte est injectée en cours de tour via le hook `PostToolUse`. |
| Validations humaines | Nœuds placés dans le blueprint, escalades de god, demandes de permission de Claude Code, budget dépassé. |
| Git | Un worktree (copie de travail sur sa propre branche) par équipe. Les agents d'une même équipe partagent le worktree, avec des réservations de fichiers. Une phase finale d'intégration fusionne les branches et comble les trous. |

---

## Liste des travaux

### A. Fondations
1. Dépôt, conventions et Taskfile : build, test, lint, dev, release.
2. Organisation Go : `cmd/` (démon, binaire des hooks), `internal/` par domaine.
3. Application Next.js (App Router, TypeScript strict), servie par le backend Go.
4. Spec OpenAPI écrite en premier, avec génération des schémas JSON et des structures Go. Validation des entrées par les schémas.
5. Outillage qualité : golangci-lint, `go vet`, `tsc --noEmit`, ESLint, CI (pipeline GitLab).
6. ADRs initiaux :
   - construire plutôt qu'assembler ;
   - Go + Next.js ;
   - SQLite ;
   - la CLI dans un PTY plutôt que l'Agent SDK ;
   - le modèle de channels unidirectionnels ;
   - le protocole de messages inspiré du hive.
7. Gestion de la configuration et des secrets : variables d'environnement, aucun secret dans les prompts ni dans les logs.

### B. Modèle de domaine et stockage
8. Entités à modéliser :
   - catalogue : AgentDefinition, Team ;
   - workflow : Blueprint (nœuds, flux, channels, validations), Project (dépôt et appareil) ;
   - exécution : Run (une exécution de blueprint sur une feature), AgentInstance, Worktree ;
   - travail : Ticket, Deliverable, Comment ;
   - communication : Message, Mailbox (normale / urgente), Thread ;
   - partage et contrôle : BoardEntry (décision ou question ouverte), Approval, Budget, Event.
9. Schéma SQLite et migrations. Journal d'événements où l'on ne fait qu'ajouter, source de l'audit et du temps réel.
10. Curseurs par consommateur, pour que le routeur et les hooks soient idempotents (aucun message traité deux fois).

### C. Catalogue d'agents (« recruter l'agent comme on le veut »)
11. Spec du format : markdown, en-tête YAML compatible avec les subagents, champs propres à l'outil (modèle, outils, permissions, MCP, skills, budget, rôle).
12. **Bundle de modèles d'agents fourni avec l'outil, prêts à l'emploi** : god, archi, sécu, dev (backend et frontend), rédacteur BDD (à partir des AC uniquement), exécuteur de tests de validation, pentester, métrologue, documentaliste, intégrateur, reviewer, etc. Ce bundle est rédigé, testé et versionné avec l'outil.
13. **Parcours de recrutement** :
    - partir d'un modèle et le recruter tel quel, ou le personnaliser (prompt, outils, permissions, modèle, budget, skills, MCP) ;
    - modifier un modèle ;
    - dupliquer un agent ;
    - créer un agent de zéro ;
    - affecter l'agent recruté à une ou plusieurs équipes.
    Le modèle d'origine reste intact, et l'agent recruté garde la trace du modèle dont il vient pour pouvoir récupérer ses mises à jour.
14. Validation par schéma et versionnement des agents recrutés.
15. Sources secondaires d'import : `~/.claude/agents` en local, et catalogues en ligne (wshobson/agents, VoltAgent, marketplaces de plugins), avec la source, la licence et la version d'origine conservées.
16. **Relecture de sécurité de tout agent importé de l'extérieur** : diff du prompt, alertes sur les outils trop larges et sur les serveurs MCP référencés (risque d'injection de prompt), validation explicite avant toute activation.

### D. Équipes
17. Définition d'une équipe : objectif commun, membres, consignes partagées. Chaque agent reçoit le contexte de l'équipe en plus de ses spécificités.
18. Channel interne à l'équipe ouvert par défaut. Option : un lead d'équipe.
19. Équipes fournies par défaut : specs (archi + sécu), dev, validation, pentest, métro, intégration.

### E. Éditeur de blueprint (sans code)
20. Canvas de nœuds et de liens. Bibliothèque à choisir (React Flow probablement, nouvelle dépendance à signaler).
21. Types de nœuds : god, équipe, validation humaine, début, fin, intégration.
22. Deux types de liens :
    - **flux de workflow** : passage de relais avec ce qui est transmis (besoin, specs, code) ;
    - **channel** : unidirectionnel, définit qui a le droit de solliciter qui.
23. Règles de validation du blueprint : nœuds orphelins, cycles de workflow, validations humaines inatteignables, channel vers une équipe inexistante.
24. Export et import YAML, versionnement, blueprint « livraison de feature » fourni par défaut.

### F. Moteur d'exécution
25. Instancier un Run à partir d'un blueprint, d'un projet et d'un besoin validé.
26. Machine à états des phases : démarrage des équipes, passages de relais, attente aux validations humaines, parallélisme entre équipes.
27. Phase d'intégration : fusion des branches des équipes, résolution des conflits par les agents, comblement des trous, dernière validation.
28. Pause, reprise et arrêt d'un Run. Arrêt d'urgence global.
29. Reprise après redémarrage du backend : réattacher ou reprendre les sessions (`claude --resume`) et le routeur.

### G. Session god
30. Chat d'expression du besoin : accès à la doc (ragflow, GitLab et autres MCP), questions, production du besoin et des AC, validation par l'humain.
31. Délégation directe : créer les tickets, les découper, les attribuer à une équipe ou à un agent.
32. Appels d'offres (« Contract Net ») : annoncer une tâche, recevoir les réponses des agents candidats, choisir et attribuer.
33. Arbitrage des conflits. Politique d'escalade dans son prompt système : périmètre, conflit, dépense, plafond d'échanges atteint.
34. Respect du cadre du blueprint : god orchestre à l'intérieur des phases et des channels déclarés.

### H. Messagerie (façon hive)
35. Schéma des messages : id, fil (`conversation`), `in_reply_to`, émetteur, destinataire (agent, équipe ou `broadcast`), type (`request`, `inform`, `propose`, `query`, `agree`, `refuse`, `done`), priorité (normale / urgente), référence au ticket, compteur d'échanges (`hops`), réponse attendue (`requires_reply`), besoin d'un humain (`needs_human`).
36. Routeur qui livre dans les bannettes normale ou urgente, et **applique les channels** :
    - refus si aucun channel n'est déclaré ;
    - réponse autorisée dans un fil ouvert par l'autre partie.
37. Anti-boucle : seuls `request`, `query` et `propose` obligent à répondre, et au-delà du plafond de `hops`, escalade vers god.
38. Fils de discussion, archive des messages traités, journal d'audit.

### I. Livraison dans Claude Code
39. Serveur MCP exposé aux agents, avec les outils : `send`, `reply`, `inbox`, `board_read` / `board_propose`, `ticket_*`, `deliverable_add`, `bid`, `reserve_files`.
40. Binaire des hooks (Go) appelé par Claude Code :
    - `Stop` : livre la bannette normale (bloque l'arrêt tant qu'il reste des messages non lus) ;
    - `PostToolUse` : injecte les `request` et alertes urgentes ;
    - `PreToolUse` : remonte le statut et l'activité de l'agent.
41. Réponse immédiate aux questions urgentes : `claude -p --resume <session> --fork-session --tools ""`. Règle « réponds `ESCALATE` si aucune décision n'a encore été prise sur ce point ». Copie dans la bannette du destinataire et possibilité de correction.
42. Réveil d'un agent inactif qui reçoit un message. Mécanisme à trancher : envoyer un prompt dans le PTY, ou reprendre la session en mode sans interface.
43. Génération des `.claude/settings.local.json` de chaque agent (hooks, permissions, MCP), sans toucher aux settings globaux.

### J. Tickets
44. Création, lecture, modification, suppression. Statuts, propriétaire (agent ou équipe), dépendances bloquantes, lien vers les AC.
45. Livrables : fichiers, commits, rapports (tests, pentest, métro), chacun rattaché à un ticket.
46. Commentaires d'agents et d'humains.
47. Timeline d'un ticket qui rassemble messages, événements, appels d'outils, coûts, livrables et diffs.

### K. Tableau partagé et base de connaissances
48. Tableau partagé du Run : décisions, questions ouvertes, hypothèses. God est le seul rédacteur, tous les agents peuvent proposer.
49. Le **documentaliste est un agent du catalogue comme un autre**, avec un rôle particulier : construire une **base de connaissances** à partir de tous les livrables et de toutes les décisions (specs, ADR, rapports de tests, pentest et métro, récapitulatifs de Run). L'outil doit seulement lui fournir l'accès en lecture aux livrables, au tableau partagé et aux événements (via le serveur MCP), et l'endroit où écrire.
50. Format et emplacement de la base de connaissances : markdown versionné dans le dépôt du projet, ou dans un dépôt dédié. Option : alimentation automatique d'un dataset ragflow, pour que god et les agents la consultent lors des Runs suivants.

### L. Runtime des agents
51. Interface d'adaptateur de runtime, avec Claude Code comme première implémentation.
52. Lancement de `claude` dans un PTY (`creack/pty`) : worktree, environnement (identifiants d'agent, de Run et de ticket, attributs OTel, `ANTHROPIC_AUTH_TOKEN`), modèle, prompt système composé (agent + équipe + protocole).
53. Cycle de vie : démarrage, arrêt, redémarrage, reprise, détection de crash, redimensionnement du terminal, limite d'agents simultanés.
54. Statut de chaque agent (actif, en attente de réponse, bloqué par une permission, inactif, en erreur), alimenté par les hooks.
55. Flux du PTY vers l'UI par WebSocket, et saisie de l'humain dans le terminal d'un agent.

### M. Git et espaces de travail
56. Sélection et clonage d'un projet sur l'appareil où tourne le backend (local ou VPS).
57. Un worktree par équipe, branches nommées par Run et par équipe.
58. Réservations de fichiers à l'intérieur d'une équipe (avec expiration) pour éviter que deux agents modifient le même fichier.
59. Fusion de la phase d'intégration, détection des conflits, remise aux agents intégrateurs.
60. Optionnel par la suite : création des MR GitLab et recopie des décisions et findings en commentaires.

### N. Isolation et sécurité
61. Équipe validation sans accès au code en phase 1 : permissions `deny Read` sur les sources, et worktree qui ne contient que les AC et l'interface publique.
62. Capter les demandes de permission (hook à confirmer) et les envoyer vers la file d'approbations de l'UI.
63. Secrets : jamais dans les messages, les tickets ou les logs. Masquage dans l'affichage des PTY si possible.
64. Accès distant pour le mode VPS : jeton d'API et TLS, ou Tailscale / WireGuard.

### O. Budgets et garde-fous
65. Collecte des coûts et des tokens par agent, équipe, Run et ticket (OTel ou hooks).
66. Budgets par agent, équipe et Run, avec seuils d'alerte et pause automatique en attendant la décision de l'humain.
67. Plafonds : `hops`, nombre d'agents simultanés, nombre de blocages par le hook `Stop`, durée maximale d'un tour.

### P. Observabilité et traçabilité
68. Export OTel de Claude Code avec des attributs de ressource (Run, équipe, agent, ticket) et des spans propres au backend.
69. Choix du stockage et de la visualisation des traces : Langfuse auto-hébergé, ou vue intégrée.
70. Lecteur JSONL optionnel et tolérant pour la vue « raisonnement » et l'historique des sessions terminées, isolé dans un module avec des tests de non-régression.

### Q. API backend
71. REST pour : catalogue, équipes, blueprints, runs, tickets, messages, approbations, budgets.
72. Temps réel : événements par WebSocket ou SSE, flux des PTY.
73. Authentification locale par jeton, renforcée en mode VPS.

### R. Front (Next.js)
74. Tableau de bord des runs.
75. Chat avec god (expression du besoin, validation du besoin et des AC).
76. Catalogue d'agents : liste, duplication, éditeur markdown et en-tête, création.
77. Éditeur d'équipes.
78. Éditeur de blueprint.
79. Vue d'un Run : équipes et agents avec leur statut en direct, phase en cours.
80. Vue courrier : bannettes normale et urgente de chaque agent, fils de discussion, animation du messager en option.
81. Tableau des tickets (kanban et dépendances) et détail d'un ticket (timeline, livrables, commentaires).
82. Détail d'un agent : terminal xterm.js, transcript, livrables, coûts.
83. Visionneuse de diffs (Monaco) par équipe et par ticket.
84. File d'approbations (validations du blueprint, escalades, permissions, budgets).
85. Tableau partagé et documentation produite.
86. Coûts et budgets.
87. Paramètres : projets, appareil, runtimes.

### S. Persistance et reprise
88. Snapshot et restauration d'un Run (état, sessions, curseurs).
89. Sauvegarde de la base SQLite, surtout en mode VPS.

### T. Déploiement
90. Mode local : un binaire Go qui embarque le front, plus les prérequis (git, `claude`).
91. Mode VPS : service systemd ou Docker, authentification Claude Code sur le serveur, accès réseau sécurisé, sauvegardes.
92. Choix d'architecture du front : export statique de Next.js servi par Go, ou serveur Next.js séparé.

### U. Tests et qualité
93. Tests unitaires Go sous forme de tables (routeur, channels, machine à états, budgets).
94. **Faux runtime `claude`** : une CLI bouchon qui simule les sessions et les hooks, pour tester l'ensemble sans coût d'API.
95. Tests de contrat de l'adaptateur Claude Code contre la vraie CLI (hooks, `--fork-session`), à relancer à chaque montée de version.
96. Tests de bout en bout de l'UI avec Playwright.

### V. Documentation
97. `PROTOCOL.md` injecté dans le prompt des agents (comment communiquer, channels, tickets, livrables).
98. Documentation utilisateur : installation locale et VPS, créer un agent, une équipe, un blueprint, lancer un Run.
99. Documentation d'architecture et ADRs.

### W. Plus tard
100. Runtimes supplémentaires (Codex, Gemini CLI).
101. Intégration GitLab poussée (issues, MR, pipelines).
102. Multi-utilisateurs.

---

## Décisions ouvertes, à trancher pendant la conception
- Granularité des channels : par équipe seulement, ou aussi par agent.
- Mécanisme de réveil d'un agent inactif (prompt envoyé dans le PTY ou reprise en mode sans interface).
- Hook exact pour capter les demandes de permission.
- Front : export statique servi par Go, ou serveur Next.js séparé.
- Bibliothèque du canvas de blueprint (React Flow ?).
- Stockage des traces : Langfuse ou vue intégrée.
- Stockage des livrables : dans le dépôt du projet ou dans l'outil.
- Nom de l'outil.

## Validation globale
Un scénario de bout en bout sur un projet jouet :
1. Recueil du besoin avec god.
2. Équipe specs (archi + sécu).
3. Équipes dev et validation en parallèle, avec une question urgente du dev à l'archi traitée par embranchement de session, et un channel non déclaré correctement refusé.
4. Pentest et métro, puis intégration.
5. Le ticket montre une timeline complète, les budgets sont respectés, et toutes les validations humaines passent par l'UI.

Ce scénario est d'abord joué avec le faux runtime, puis avec la vraie CLI Claude Code.
