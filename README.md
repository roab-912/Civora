# Civora — gouvernance v0.2

Implémentation locale des jalons v0.1 et v0.2 de la [spécification](spec/Protocole-Civique-v1.md). Le noyau Go gère comptes, propositions, soutiens, scrutins, lois, rôles, attributions, révisions et politiques. Il applique les échéances et les effets de manière déterministe et conserve le journal durable.

Pour lancer la démonstration depuis PowerShell :

```powershell
.\scripts\demo.ps1
```

Elle exécute un scénario de **99 blocs** : création, amendement, abrogation et restauration d’une loi ; création, attribution, publication, révocation et restauration d’un rôle ; mise à jour constitutionnelle de la politique. Le résumé affiche une loi, un rôle, une attribution active et la révision de politique 2.

La démonstration utilise un **simulateur de développement** : les choix et engagements sont visibles. Il est compilé exclusivement avec le tag `civora_sim`, dans le binaire distinct `civsim`, et exige la genèse de test dédiée. `civnode` refuse cette genèse. Semaphore et ElectionGuard restent aux jalons v0.3 et v0.4. Aucune interface web ni réseau n’est démarré.

Prérequis : Go ≥ 1.26 et Node ≥ 24.12, npm. Les versions et dépendances utilisées sont consignées dans [le verrou de développement](spec/profiles/development.lock.json), `go.sum` et `package-lock.json`.

```sh
npm ci --ignore-scripts
npm test
go test ./... -count=1 -timeout 120s
go vet ./...
go test -tags civora_sim ./... -count=1 -timeout 180s
go vet -tags civora_sim ./...
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/activation.json -store .dev-store/activation -supported-epoch 2
go run -tags civora_sim ./cmd/civsim demo
go run -tags civora_sim ./cmd/civsim replay -fixture spec/fixtures/v0.2/governance.json -store .dev-store/governance
```

Un rejeu répété sur le même répertoire reprend les blocs durables et vérifie leur identité. Sans `-supported-epoch 2`, le scénario d’activation s’arrête avant le bloc 3. Les CLI acceptent uniquement les genèses `mode:development`, `voting_profile:inline_dev`. Les anciens scénarios v0.1 conservent leurs racines.

Sous Windows, [scripts/check.ps1](scripts/check.ps1) exécute les vérifications, les deux modes de compilation, les contrôles de reproductibilité, les démonstrations et leur reprise dans un nouveau répertoire ignoré. Les scripts utilisent Go installé dans `.tools/go` si disponible.

Les tests couvrent toutes les transitions de gouvernance, les bornes temporelles, les quorums, l’égalité, le résultat vide, les conflits concurrents, les capacités et portées, l’expiration des attributions et l’absence de réactivation d’anciens droits après restauration. Vingt scénarios distincts d’arrêt forcé couvrent comptes, activation, bulletins et effets, chacun à cinq points d’écriture. La reprise doit reproduire exactement les blocs, états, événements et marqueurs durables.

Le TypeScript conserve l’oracle indépendant des comptes v0.1 et vérifie les schémas, les seuils exacts et la racine de l’état final de gouvernance généré en Go. Il ne réimplémente pas encore l’ensemble de la machine de gouvernance et refuse explicitement d’utiliser l’oracle v0.1 avec une genèse v0.2.

Les [fixtures](spec/fixtures/) contiennent **des clés privées publiques de test**, explicitement étiquetées `TEST_ONLY`. `npm run fixtures` régénère v0.1 ; `go run -tags civora_sim ./cmd/civsim fixtures` régénère v0.2. L’option `fixtures --check` vérifie v0.2 sans réécriture. `npm run schemas` régénère les schémas JSON Schema 2020-12, partagés par les deux langages.

Les décisions v0.2 sont consignées dans [0005 — gouvernance](spec/decisions/0005-gouvernance-v0.2.md) et [0006 — simulation et vérification](spec/decisions/0006-simulation-v0.2.md). Les ports `EligibilityProvider`, `VotingCrypto` et `EffectAdapter` sont isolés dans `internal/ports` et `web/src/ports.ts`. L’adaptateur d’effet actuel est exclusivement `ledger_only_v1`, sans action extérieure.

Les rotations de validateurs et gardiens, l’approbation et l’armement des mises à jour, CometBFT, la PWA, le déploiement et les outils d’urgence restent aux jalons correspondants. Le stockage local est prévu pour un seul écrivain ; les tests Windows qualifient l’arrêt de processus, pas une coupure électrique.
