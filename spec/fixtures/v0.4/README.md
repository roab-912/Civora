# Fixtures v0.4

Les fichiers `envelopes.json` et `*.envelope.json` restent des **TEST_ONLY_ENVELOPES_NOT_CRYPTOGRAPHIC_FIXTURES** : 59 cas de transport, dont trois enveloppes acceptées et 56 refus. Leurs marqueurs opaques ne sont jamais des bulletins valides.

Les fichiers suivants sont des données cryptographiques réelles de développement :

- `crypto.json` : deux cérémonies 3/5 distinctes, huit bulletins produits par le SDK, deux agrégats et preuves publiques du total ; aucun secret.
- `reject-crypto.json` : 38 altérations et cas de refus partagés par Go et TypeScript.
- `scale.json` : preuve de connaissance de la véritable sélection SDK, preuve Semaphore de profondeur 26, routage sur 32 partitions et copie avec preuve valide d’une autre identité. Le contexte du point de contrôle est fourni par la fixture, pas authentifié par BFT.
- `memberships.json` : preuves Semaphore de profondeur 16 liées aux vrais paquets chiffrés du scénario inline.
- `replay-mixed.json`, `replay-all-no.json` : neuf blocs par scénario, états, événements et racines attendus du moteur Go réel.
- `replay-source.json` : entrées publiques déterministes permettant de régénérer ces deux scénarios.

**PUBLIC TEST SECRETS ONLY.** `actors.TEST_ONLY.json` expose les clés civiques et les identités de test. `oracle.TEST_ONLY.json` contient le corpus source du SDK, les nonces de sélection et les choix de référence. Les polynômes des gardiens sont déterministes depuis les étiquettes publiques du harnais. Ne jamais utiliser ces clés pour un vrai vote, ni inclure ces fichiers dans une archive publique de scrutin.

Les preuves probabilistes figées sont réutilisées et vérifiées ; la régénération mathématique déterministe ne signifie pas régénération implicite de leur aléa. Les tirages nouveaux sont explicites. Voir [le harnais](../../../tools/electionguard/README.md), [l’annexe](../../interop/electionguard-v0.4.md) et [le rapport](../../reports/v0.4-interop.json).

`npm run fixtures:electionguard:check` contrôle les conversions et fixtures TypeScript sans réécrire les données suivies. `go run ./cmd/civcrypto fixtures -check` contrôle états et racines Go. Le script de contrôle peut recréer le fichier temporaire de requête dans `.tools`, à partir des données déterministes.