# 0007 — Suite Semaphore v0.3 et verrou d’artefacts

Statut : adopté pour le développement v0.3, 4 novembre 2026. Références : §§4.1, 7.1–7.3, 12, 14.3, 15.2.

Les packages `@semaphore-protocol/{identity,group,proof}` sont fixés à 4.14.5 dans `package-lock.json`. Le commit npm publié est `dbab5553c486a09d5e1042e5271e3316224cc176`. Ce SDK référence explicitement les circuits de version **4.13.0**. Les profondeurs prises en charge sont 16 et 26 ; aucune URL `latest` ni résolution d’artefact fournie par un votant n’est utilisée.

`spec/crypto/semaphore-v4.lock.json` consigne origine, taille et SHA-256 des deux WASM, des deux zkey et des deux clés de vérification. Les clés extraites du zkey ont été comparées aux paramètres distribués avec le SDK, pour les champs de vérification utilisés. Les origines sont le dépôt officiel Semaphore, npm et `snark-artifacts.pse.dev/semaphore/4.13.0/`. Les grands fichiers sont téléchargés dans `.tools/` ; les clés de vérification et le verrou sont versionnés.

Go intègre les clés de vérification et contrôle leurs empreintes avant utilisation. Le Web Worker utilise uniquement les chemins d’artefacts de sa propre origine et vérifie les trois empreintes et tailles avant de charger le WASM ou le zkey. Une erreur ne déclenche pas le téléchargement implicite du SDK ou un repli de profondeur. Le contrôle du verrou compare aussi le module navigateur, les clés et le SDK installé. Recréer un verrou est une opération explicite de maintenance `scripts/lock-semaphore.mjs --update`, à revoir avec ses vecteurs.

Le navigateur génère l’identité et la preuve dans un Web Worker. Il reçoit une identité locale/importée, un seul chemin et un contexte d’instantané attendu ; il ne télécharge pas les autres engagements. Les secrets restent dans le Worker. Le coffre et la sauvegarde chiffrée de v0.7 ne sont pas implémentés à ce jalon ; les identités de test sont publiques et les nouvelles identités de session restent en mémoire.

Les champs de `membership` sont ceux du SDK, avec profondeur et coordonnées sous forme de chaînes : `merkleTreeDepth`, `merkleTreeRoot`, `nullifier`, `message`, `scope`, `points`. Le tableau `points` contient huit coordonnées : `[A.x,A.y,B.x.imag,B.x.real,B.y.imag,B.y.real,C.x,C.y]`. L’adaptateur Go reconstruit les tableaux SnarkJS en inversant les paires G2, comme l’outil officiel d’empaquetage.

Les quatre entrées publiques de Groth16 sont exactement `[root,nullifier,SDKHash(message),SDKHash(scope)]`, dans cet ordre. `SDKHash(x)=uintBE(keccak256(uint256BE32(x))) >> 8`. Le message et le scope transmis au SDK restent les entiers de 248 bits du §7.3, dérivés par SHA-256 avec les domaines `vote/message` et `vote/scope`. Confondre ce Keccak interne avec SHA-256 ou Poseidon invaliderait l’interopérabilité.

Les racines et nullifiers sont bornés au corps scalaire BN254 ; les coordonnées au corps de base BN254. Les encodages décimaux non canoniques, nombres JSON, champs inconnus, points hors champ et points à l’infini sont refusés. La vérification de courbe et de sous-groupe est celle du vérificateur Go verrouillé `go-rapidsnark/verifier v0.0.5`. Les vecteurs sont vérifiés aussi avec le SDK 4.14.5. Cela ne constitue pas un audit de ces bibliothèques.

Le contexte attendu fournit réseau, scrutin, epoch, suite, profondeur et racine. L’adaptateur recalcule scope et message et refuse une preuve relabellisée pour un autre scrutin, réseau ou contenu. L’autorisation de l’epoch et du module vient du registre appelant : le format Semaphore ne contient pas lui-même une preuve de finalité du manifeste ni un epoch autonome. Un scrutin garde le contexte fixé à son ouverture. Les barrières de version existantes empêchent de réutiliser ce scrutin dans un nouvel epoch.

Le port `MembershipVerifier` est distinct de `VotingCrypto`. Le contrôle de membership est raccordé au moteur pour les profils qui l’activent, mais ne remplace jamais le contrôle du bulletin et du dépouillement. Sans adaptateur ElectionGuard valide, aucun bulletin réel n’est compté. L’outil de v0.3 vérifie des octets de développement comme contenu lié à la preuve ; il ne les présente pas comme un chiffré ElectionGuard.

Les identités, arbres, contextes et octets de contenu des fixtures sont reproductibles à partir de libellés publics de test. Groth16 est probabiliste : les preuves figées sont conservées et vérifiées, plutôt que de remplacer son aléa par un générateur déterministe de production. `--new-proofs` les régénère explicitement. Les graines et exports portent `TEST_ONLY` et ne proviennent d’aucun utilisateur.

Sources primaires : [Semaphore, code du SDK](https://github.com/semaphore-protocol/semaphore), [circuits officiels](https://snark-artifacts.pse.dev/semaphore/4.13.0/semaphore-16.wasm), [vérificateur Go](https://github.com/iden3/go-rapidsnark). Les versions et empreintes utilisées sont celles du verrou et des fichiers de dépendances du dépôt.
