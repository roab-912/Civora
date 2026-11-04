# Fixtures d’enveloppe v0.4

**TEST_ONLY_ENVELOPES_NOT_CRYPTOGRAPHIC_FIXTURES**

Ces fichiers ne contiennent aucun bulletin, total ou cérémonie ElectionGuard valide. Les contenus opaques sont volontairement des marqueurs de test. Le fichier `context.json` du paquet de cérémonie contient des nombres JSON et un LF pour démontrer la préservation des octets externes ; ce n’est pas un contexte de scrutin utilisable. Aucune clé nouvelle n’est générée ici.

`envelopes.json` contient les 59 vecteurs communs Go/TypeScript : trois enveloppes acceptées uniquement comme transport, 56 refus d’encodage, de structure, de chemin ou de liaison civique. Les limites de tailles sont exercées aux bornes dans les tests, sans ajouter de fichiers volumineux. Les empreintes d’inspection attendues incluent toujours `cryptography_verified:false` et `suite_qualified:false`.

`*.envelope.json` contient les octets JCS décodés de chaque exemple de transport, pour la commande d’inspection. Ils ne permettent pas d’ouvrir ni de dépouiller un scrutin.

Reproduction : `npm run fixtures:electionguard`. Contrôle sans écriture : `npm run fixtures:electionguard:check`. Les fixtures cryptographiques manquantes et leurs critères restent recensés dans [le rapport](../../reports/v0.4-interop.json).
