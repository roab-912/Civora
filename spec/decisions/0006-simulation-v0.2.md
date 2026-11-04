# 0006 — Simulation isolée et vérification v0.2

Statut : adopté le 4 novembre 2026. Références : §§7, 11, 12. Cette décision complète 0003–0004 sans introduire de cryptographie de vote public.

Les ports `EligibilityProvider`, `VotingCrypto` et `EffectAdapter` sont purs et séparés du moteur. Ils ne peuvent consulter horloge locale, réseau, fichier ou service externe pendant une transition. Le fournisseur construit l’instantané, le vérificateur fixe le contexte et contrôle les bulletins et totaux, l’adaptateur d’effet retourne un reçu déterministe. Le noyau garde les contrôles de phase, domaine, état des suites, unicité, compte, quorum et calendrier.

L’adaptateur d’effet fourni est `ledger_only_v1`. Il produit `D("effect/ledger",[network_id,proposal_id,action,effective_ms])` comme référence ; les mutations métier restent transactionnelles dans le registre. Aucun appel à une institution extérieure n’est effectué.

Le simulateur est dans `internal/simulation`, dont tous les fichiers portent `//go:build civora_sim`. Son seul point d’entrée est le binaire distinct `cmd/civsim`, lui aussi conditionné au tag. `cmd/civnode` utilise toujours `core.New`, qui refuse `crypto_profile:simulation_v0_2`. La vérification automatique contrôle l’absence de `internal/simulation` dans les dépendances de `civnode`.

Le simulateur exige le réseau déterministe `SHA256(UTF8("CIVORA PUBLIC SIMULATION NETWORK v0.2 — NOT A PILOT"))`, le mode `development` et le profil `simulation_v0_2`. Les identifiants de ses modules commencent par `dev.sim`. Leurs empreintes d’artefacts sont des marqueurs de test, pas des binaires de diffusion. Les véritables suites Semaphore/ElectionGuard ne sont jamais déclarées actives par ce simulateur.

Les paquets du simulateur sont explicitement différents de ceux d’ElectionGuard. Ils exposent le choix et, dans les points de la preuve de développement, l’engagement utilisé. Les contrôles de contexte engagent réseau, scrutin, instantané, cérémonie et octets du bulletin. Les domaines `simulation/group`, `simulation/nullifier`, `simulation/membership`, `simulation/tally`, `simulation/confirmation` ne fournissent aucune propriété de secret, d’anonymat ou de preuve de connaissance. Leurs champs de test sont des entiers issus des 31 premiers octets d’une empreinte, avec profondeur déclarée 16 pour exercer les enveloppes. Ils ne sont pas des points de courbe ou des preuves Semaphore.

Le contexte de la question conserve le domaine `poll/question`. Les `scope` et `message` de l’enveloppe suivent les domaines du §7.3. L’agrégat du simulateur est une chaîne décimale de total Oui ; c’est un format de test. Les futurs agrégats chiffrés et leurs encodages devront être verrouillés avec ElectionGuard au jalon v0.4. Aucun parcours ne remplace une preuve manquante par ce simulateur à l’exécution.

Le générateur Go du simulateur produit `spec/fixtures/v0.2/governance.json`, ses racines, événements et état final, ainsi que `keys.TEST_ONLY.json`. Les graines publiques sont dérivées de libellés de test fixes avec SHA-256. Les horodatages des blocs sont fournis par le scénario, sans lecture du système. Les politiques conservent leurs bornes réelles ; le scénario avance les millisecondes de bloc pour exercer les délais.

Le scénario couvre 99 blocs et douze décisions adoptées, dont une constitutionnelle. Les tests couvrent aussi les états terminaux négatifs, les fenêtres exactes, les limites de file, les conflits, les droits historiques d’un instantané, les clés de gardiens épinglées, l’expiration, les capacités et la portée. Une publication conserve les révisions inchangées.

Le TypeScript conserve l’oracle complet des comptes v0.1. Pour v0.2, il expose les types d’enveloppes, les ports et le calcul exact des seuils ; il valide le scénario Go avec les mêmes schémas et recalcule indépendamment la racine de son état final et tous ses identifiants d’événements. Il refuse explicitement de rejouer une genèse v0.2 dans l’ancien oracle de comptes. Il n’est pas un deuxième moteur complet de gouvernance.

La reprise durable utilise une fabrique d’adaptateurs explicite, identique pour exécution et rejeu. Les dix injections supplémentaires de mort de processus s’effectuent pendant l’admission atomique de bulletins et l’application d’une loi, aux cinq points du journal v0.1. Les exports complets doivent être identiques à une exécution ininterrompue. Le contrat reste un écrivain par répertoire et une mort de processus, sans qualification de panne électrique Windows.

Le jalon s’arrête à v0.2. Les artefacts et preuves réelles, les partitions et le consensus, le coffre de clés, la PWA, le déploiement et les outils d’urgence restent aux jalons suivants.
