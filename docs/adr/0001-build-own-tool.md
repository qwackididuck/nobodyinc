# ADR-0001 — Construire notre propre outil d'équipes d'agents

- Statut : accepté
- Date : 2026-11-11

## Contexte

Nous voulons faire travailler plusieurs équipes d'agents Claude Code (specs archi+sécu, dev, validation BDD, pentest, métrologie, documentation), capables de communiquer à l'intérieur d'une équipe et entre équipes, avec traçabilité par ticket et validations humaines, le tout supervisé depuis une interface graphique.

Outils évalués (novembre 2026) :

| Outil | Pourquoi il ne convient pas seul |
|---|---|
| Paperclip | Testé : séquentiel, piloté par réveils périodiques et tickets, lent pour les échanges |
| OpenRig (+ Herdr) | Messagerie `rôle@équipe` solide, mais interface trop orientée terminal, sans budgets ni gouvernance |
| munder-difflin | Bon protocole de messages (le « hive »), mais habillage « The Office », pas de notion d'équipe, livraison des messages seulement en fin de tour |
| oh-my-claudecode | Dépend du flag expérimental Agent Teams |
| Orca, agent-orchestrator, omnigent, Orkas | Agents en parallèle, sans communication entre eux |
| MCP Agent Mail, BMAD, LangGraph, CrewAI… | Briques partielles (messagerie seule, méthode seule, ou hors Claude Code) |

## Décision

Construire un outil dédié qui reprend les meilleures idées existantes :
- les équipes et l'adressage `rôle@équipe` d'OpenRig ;
- le protocole de messages du hive (types de messages inspirés de FIPA, plafond d'échanges, bannettes) ;
- en ajoutant une bannette urgente (embranchement de session, injection en cours de tour), un blueprint sans code avec channels unidirectionnels, une session god avec appels d'offres, et des tickets avec livrables.

L'outil reste une couche fine au-dessus de Claude Code, appuyée sur ses interfaces documentées (hooks, options CLI, OTel).

## Conséquences

- Coût de développement et de maintenance assumé ; périmètre détaillé dans `docs/roadmap.md`.
- Dépendance aux évolutions de Claude Code limitée par un adaptateur de runtime isolé et des tests de contrat.
- Si Agent Teams devient stable, il pourra devenir un runtime supplémentaire plutôt qu'un concurrent.
