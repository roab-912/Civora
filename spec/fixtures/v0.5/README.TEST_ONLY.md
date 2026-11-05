# Fixtures publiques de développement v0.5

Ces fichiers contiennent des contextes et identités **TEST_ONLY**, jamais une autorité pour un scrutin réel. Les clés civiles et les nonces SDK viennent des fichiers `../v0.4/actors.TEST_ONLY.json` et `../v0.4/oracle.TEST_ONLY.json`. Les clés de consensus, P2P et d'archive se reproduisent avec `network.TestKey`, selon la décision [0011](../../decisions/0011-consensus-partitions-v0.5.md).

- `ballots.json` : cinq vrais bulletins du SDK, preuves Semaphore 26 et preuves RFC 8235 ; une copie avec un autre nullifier ; une troisième enveloppe cryptographiquement valide pour tester la limite de deux tentatives. Les preuves Groth16 figées sont réutilisées en vérification.
- `transport.json` : genèses, blocs, racines, ouverture authentifiée, points intermédiaires et scellés des huit partitions. Six dépôts sont admis : cinq comptés, une copie rejetée. Les partitions vides fournissent aussi leur scellé. Le registre principal vérifie un total global de trois Oui et deux Non, qui reste provisoire.
- `rotation.app.json` : rotation d'un siège annoncée à H=4, effective à H=6. La nouvelle identité a un fichier de signature distinct préinitialisé ; l'ancienne conserve son état. `serve -key-generation 1` sélectionne cette identité, sans remise à zéro.
- `activation.app.json` : plan v0.1 préarmé de développement, activation fixe à H=3 avec une fenêtre compatible avec le temps BFT réel. Cinq nœuds capables de l'epoch 2 progressent ; deux nœuds limités à l'epoch 1 s'arrêtent avant de signer H=3.
- `zip215.json` : commit de quorum avec un encodage R non réduit, accepté selon les règles Ed25519 natives ZIP-215. Le remplacement de cet encodage dans la signature est refusé par les vérificateurs Go et TypeScript.

Les commits de `transport.json` sont **fabriqués pour la fixture**, avec de vraies signatures Ed25519 de cinq sièges. Ils permettent d'avancer les calendriers sans attendre des heures. Le test `npm run test:consensus` produit séparément des commits issus de vrais processus CometBFT, dans `.tools/acceptance/`, et vérifie aussi une preuve d'état native avec Go et TypeScript. Le reçu testé établit l'admission ; il ne promet pas le comptage d'un bulletin invalide.

Vérifier sans réécriture :

```sh
npm run fixtures:partitions:check
go run ./cmd/civnet fixtures -check
```

Reconstruire les données déterministes en conservant les preuves probabilistes figées :

```sh
npm run fixtures:partitions
go run ./cmd/civnet fixtures
```

Un nouveau tirage Groth16 exige `node scripts/generate-partition-ballots.mjs --new-proofs`, avec les artefacts locaux contrôlés par le verrou Semaphore. Il modifie les octets, signatures et racines qui les engagent et impose une régénération explicite de `transport.json`.

Aucun miroir d'archive, service public, audit externe, PWA, déploiement ou outil d'urgence n'est ajouté par ces fixtures. La certification, les relais et les archives sélectives restent aux jalons suivants.
