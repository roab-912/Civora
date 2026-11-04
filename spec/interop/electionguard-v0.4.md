# ElectionGuard v0.4 — mapping normatif de développement

Cette annexe complète les structures ouvertes de la sérialisation officielle 0.0.1. Elle conserve le **design 2.0.0** et définit une **sérialisation Civic propre**, `eg-civic-compact-1`, pour `eg-2.0-civic-1`. Elle ne prétend pas que le format `json2` du SDK est le format officiel 0.0.1. Voir [0010](../decisions/0010-electionguard-v0.4.md).

## Types et enveloppes

Les fichiers compacts sont du JCS Civic strict : champs supplémentaires, nombres JSON, doublons et octets non canoniques refusés. Tous les champs du schéma sont obligatoires ; aucun champ secret n’est toléré, même `null`.

| Type | Encodage |
|---|---|
| P | Entier non signé de 512 octets en ordre réseau, Base64url strict, 683 caractères. `0<x<p` et `x^q=1 mod p`. |
| Q | Entier non signé de 32 octets en ordre réseau, Base64url strict, 43 caractères. `0≤x<q`. |
| Empreinte EG | 32 octets Base64url, sans réduction modulo q sauf pour calculer un challenge scalaire. |
| Identifiant Civic | 32 octets hexadécimaux minuscules, 64 caractères. |
| Entier applicatif | Chaîne décimale canonique, bornée en uint64 ou constante du champ. |

Le groupe 4096/256 bits et son générateur viennent des [paramètres verrouillés](../crypto/electionguard-v2.parameters.json). Aucun groupe alternatif dans les paquets. Les bits de bourrage Base64url doivent être nuls et le réencodage exact, sans `=`. L’identité est permise pour un agrégat, un déchiffrement partiel et `K^0`, mais refusée pour les clés de coefficients, la clé jointe, l’alpha d’une sélection, le c0 des données de concours et l’engagement d’indépendance.

L’enveloppe est le Base64url de JCS `{files,suite}`, avec `suite:"eg-2.0-civic-1"`. Chaque fichier est `{bytes,path}` ; ses octets sont préservés sans réencodage. Chemins ASCII uniques et triés, liste exacte selon le paquet :

| Paquet | Chemins dans l’ordre |
|---|---|
| Cérémonie | `context.json`, `guardians/01.json` à `guardians/05.json`, `link.json`, `manifest.json`. |
| Bulletin | `ballot.json`. |
| Total | `tally.json`. |

Aucun chemin n’est extrait ou résolu comme URL. Plafonds locaux : enveloppe décodée 128 Kio, fichier 64 Kio, somme des fichiers 90 Kio ; enveloppe scale complète 256 Kio. Les plafonds du bloc et de la transaction restent indépendants. L’inspection d’une enveloppe ne vérifie aucune preuve cryptographique.

## Cérémonie

`link.json={network_id,poll_id,proposal_id,question_digest}` est du JCS Civic. Les trois identifiants doivent égaler le contexte figé ; `question_digest=D("poll/question",[network_id,poll_id,proposal_id])`. Les cinq attestations civiques restent celles du §7.2, sur le SHA-256 du paquet décodé complet.

`manifest.json` contient les octets JSON exacts du [modèle](../crypto/electionguard-v2.manifest-template.json), où `POLL_ID` est remplacé par le poll_id hexadécimal. Il correspond à `Manifest`/`ManifestJson` du SDK : petits entiers JSON externes, clés triées, une élection `election_scope_id=poll_id`, un district et un style `civic`, un concours `civic_question` de séquence 0, une sélection `yes_bit` de séquence 0 et limite 1. Aucun parti, contact, candidat supplémentaire ou écriture libre. Les dates communes 1970–9999 sont des métadonnées ; le calendrier opposable reste celui du registre. Le texte de proposition est lié par `link.json`.

| Champ de context.json | Correspondance SDK / calcul |
|---|---|
| `encoding` | Constante `eg-civic-compact-1`. |
| `number_of_guardians`, `quorum` | `ElectionConfig.numberOfGuardians=5`, `quorum=3`, chaînes `"5"`, `"3"`. |
| `parameter_base_hash` | `HP=H(ver;0x00,p,q,g)`, ver = UTF-8 de `v2.0.0` complété par zéro à 32 octets. |
| `manifest_hash` | `HM=H(HP;0x01,uint32BE(len(manifestBytes)),manifestBytes)`, sur les octets exacts. |
| `election_base_hash` | `HB=H(HP;0x02,HM,uint32BE(5),uint32BE(3))`. |
| `joint_public_key` | Produit modulo p des cinq `Guardian.publicKey()` vérifiées. |
| `extended_base_hash` | `HE=H(HB;0x12,K)`, `electionExtendedHash`. |

H est HMAC-SHA-256, avec clé publique de 32 octets. Les indices occupent 4 octets en ordre réseau, les éléments P 512 octets, Q et empreintes 32. Aucun séparateur textuel ni longueur implicite, hors les longueurs expressément définies.

| Champ de guardians/NN.json | Correspondance |
|---|---|
| `account_id` | Gardien civique figé, jamais choisi par le soumissionnaire. Les cinq identifiants sont triés ; NN va de 01 à 05. |
| `sequence_order` | `Guardian.xCoordinate`, égal à NN, chaîne de 1 à 5. |
| `coefficient_proofs` | Exactement trois preuves, polynôme de degré deux. |
| `coefficient_proofs[j].public_key` | `SchnorrProof.publicKey=K_ij`, P. |
| `…challenge`, `…response` | `SchnorrProof.challenge=c_ij`, `.response=v_ij`, Q. |

Vérifier `h=g^v K_ij^c` et `c=uintBE(H(HP;0x10,uint32BE(i),uint32BE(j),K_ij,h)) mod q`. Les cinq clés et les trois coefficients sont obligatoires. Les échanges privés suivent le SDK ; les gardiens attestent aussi avoir reçu et vérifié leurs parts. Les données publiques ne remplacent pas ce contrôle privé.

## Bulletin Oui/Non

| Champ de ballot.json | Correspondance SDK et règle |
|---|---|
| `encoding` | `eg-civic-compact-1`. |
| `ballot_id` | `EncryptedBallot.ballotId`, identifiant frais aléatoire de 32 octets, rendu en hexadécimal. Déterministe seulement dans les fixtures TEST_ONLY. |
| `ballot_style_id` | `ballotStyleId`, constante `civic`. |
| `encrypting_device` | `encryptingDevice`, constante commune `civic:` suivie de poll_id. |
| `timestamp`, `code_baux` | `timestamp=0`, chaîne `"0"` ; `codeBaux` de zéro octet, chaîne vide. Chaînage désactivé. |
| `confirmation_code`, `election_id` | `confirmationCode` et `electionId=HE`, empreintes de 32 octets. |
| `state` | `BallotState.CAST`, chaîne `CAST`. Challenge et spoil exclus du compte. |
| `contests` | Exactement un concours, sans complétion implicite. |
| `contests[0].contest_id` | `contestId`, `civic_question`. |
| `…sequence_order`, `…votes_allowed` | `sequenceOrder=0`, `votesAllowed=1`, chaînes. |
| `…contest_hash` | `contestHash`, empreinte de 32 octets. |
| `…selections` | Exactement une sélection ; absence et doublon refusés. |
| `…selections[0].selection_id`, `…sequence_order` | `selectionId=yes_bit`, `sequenceOrder=0`. |
| `…encrypted_vote.alpha`, `…beta` | `ElGamalCiphertext.pad`, `.data`, P. |
| `…proof` de sélection et de concours | Deux paires `{challenge,response}` Q, `ChaumPedersenRangeProofKnownNonce.proofs`. Les deux preuves sont exigées. |
| `…encrypted_contest_data.c0`, `.c1`, `.c2`, `.num_bytes` | `HashedElGamalCiphertext.c0` (P), `.c1` (64 octets Base64url), `.c2` (empreinte), `.numBytes` (chaîne `"62"`). Taille commune aux deux choix. |

Le chiffrement 2.0 est `(A,B)=(g^r,K^(r+m))`, `m∈{0,1}`. Pour chaque preuve 0–1, reconstruire `a_j=g^v_j A^c_j`, `b_j=K^(v_j−j c_j) B^c_j`. Exiger `c_0+c_1=uintBE(H(HE;0x21,K,A,B,a_0,b_0,a_1,b_1)) mod q`. Le concours ne contient qu’une sélection : son produit est ce même chiffré ; sa preuve reste obligatoire.

Le code de concours est `χ=H(HE;0x23,uint32BE(0),K,A,B)` ; la confirmation est `H(HE;0x24,χ)`, Baux vide. Le registre expose cette dernière en hexadécimal minuscule. Les données de concours sont chiffrées et de taille fixe ; leur MAC ne peut être authentifié avant déchiffrement par les gardiens. Elles ne participent ni au bit ni au total. Le binding Semaphore engage tous les octets du paquet, ces données incluses.

Ouvertures, `primary_nonce`, numéro de série, préchiffrés, choix en clair et champs inconnus sont refusés. Une sélection absente ne signifie jamais Non. Le registre vérifie atomiquement les doublons d’identifiant, de chiffré et de nullifier.

L’agrégat est la chaîne JCS `{alpha,beta}`, deux P initialisés à 1 puis multipliés modulo p. L’empreinte de sélection reste `D("vote/ciphertext",{alpha,beta})`. Le binding inline reste `vote/scope` et `vote/message`, sur le SHA-256 du **paquet décodé complet**.

## Total global

| Champ de tally.json | Correspondance |
|---|---|
| `encoding`, `id`, `election_id` | Format compact, `EncryptedTally.id=poll_id`, `electionId=HE`. |
| `ciphertext` | Unique agrégat `{alpha,beta}`, égal octet pour octet au chiffré de clôture. |
| `tally` | `DecryptedTallyOrBallot.Selection.tally=y`, chaîne décimale, `0≤y≤count`. |
| `b_over_m` | `Selection.bOverM`, P ; exiger `b_over_m=K^y`. |
| `proof` | `Selection.proof`, paire `{challenge,response}` Q. |
| `shares` | Trois à cinq contributions distinctes triées par indice. |
| `shares[].guardian_index` | `DecryptingTrusteeIF.xCoordinate`, chaîne de 1 à 5. |
| `…share`, `…a`, `…b` | `PartialDecryption.Mi=A^P(i)`, `.a`, `.b`, P, pour le seul agrégat global. |
| `…response` | Réponse Q du gardien au challenge collectif pondéré. Le nonce interne opaque du SDK n’est jamais publié. |

Calculer `M=B/(b_over_m)`, `a=g^v K^c`, `b=A^v M^c`. Exiger `c=uintBE(H(HE;0x30,K,A,B,a,b,M)) mod q`. Pour les gardiens disponibles, `w_i=∏(j/(j−i)) mod q`. Leur clé de vérification est `K_i*=∏_gardien∏_coefficient K_g,j^(i^j)`.

Chaque réponse vérifie `a_i=g^v_i (K_i*)^(w_i c)` et `b_i=A^v_i M_i^(w_i c)`. Exiger aussi `M=∏M_i^w_i`, `a=∏a_i`, `b=∏b_i`, `v=Σv_i mod q`. Ce transcript ne s’applique à aucun bulletin individuel. Non vaut `count−y`. Le digest de clôture et les quorums métier restent ceux du §7. Aucun total d’opérateur n’est accepté sans preuve.

## Indépendance et scale_1

`independence={commitment,response}` contient `V=g^u` (P différent de 1) et `z=u−c r mod q` (Q). Poser `C=D("scale/independence",[network_id,poll_id,group_root,nullifier,ballot_digest])`.

Le transcript RFC 8235 concatène cinq éléments, chacun précédé de sa longueur **uint32 big-endian** : g (512 octets), V (512), A (512), UserID = UTF-8 de `eg-2.0-civic-1:` suivi du nullifier décimal canonique, puis OtherInfo = C (32 octets). UserID désigne ainsi le prouveur anonyme pour ce scrutin, sans clé civile. Le challenge est `c=uintBE(SHA256(transcript)) mod q`. Vérifier `g^z A^c=V`, avec appartenance au sous-groupe et A différent de 1.

`ballot_digest` est le SHA-256 hexadécimal minuscule des octets JCS de **ballot.json** compact. `independence_digest` est le SHA-256 des octets JCS de l’objet independence. Cette frontière diffère explicitement du binding inline.

`scope=uintBE(D("scale/scope",[network_id,poll_id])[0:31])`. `message=uintBE(D("scale/message",[network_id,poll_id,ballot_digest,independence_digest])[0:31])`. Ce sont les entrées avant le Keccak interne du SDK. Le nullifier produit par le SDK doit être celui de C. La copie du corpus possède une vraie preuve d’une autre identité et échoue sur cette liaison.

L’enveloppe exacte est `{network_id,poll_id,protocol_epoch,lane_id,checkpoint_id,encrypted_ballot,independence,membership}`. Identifiants, epoch et point de contrôle doivent égaler le contexte authentifié fourni au vérificateur. Pour P partitions, puissance de deux de 1 à 256, lane_id est la chaîne décimale des `log2(P)` bits de poids fort de `D("scale/lane",[network_id,poll_id,nullifier])`, ordre réseau. Racine et profondeur Semaphore sont figées ; la fixture utilise la profondeur 26 et 32 partitions.

`VerifyScale` est pur : aucun comptage, aucune réservation de nullifier et aucune authentification BFT du contexte. Les preuves de consensus, journaux et scellés relèvent de v0.5. La preuve utilise la véritable selectionNonce exportée par le SDK, uniquement en développement dans le fichier TEST_ONLY.

## Reproduction et audit

Le verrou lie schémas, paramètres, sources, harnais et vecteurs par SHA-256. Le harnais ajoute un programme de test au SDK exact, sans modifier ses primitives. Il vérifie cérémonies, bulletins, totaux, puis le refus 2/5. Les preuves probabilistes sont figées et revérifiées ; les nouveaux tirages sont explicites.

Les fixtures publiques crypto, reject-crypto et scale ne contiennent pas de secret. Les fichiers TEST_ONLY exposent les secrets reproductibles de développement et ne sont jamais exportés. Les scénarios replay-mixed et replay-all-no produisent les résultats et effets attendus du registre avec reprise.

`civic-audit-1` contient genèse, blocs, états, événements et racines. L’auditeur Go rejoue tout depuis les données publiques, sans base interne ; l’auditeur TypeScript contrôle séparément les racines et la cryptographie, sans prétendre rejouer la gouvernance. Il faut authentifier la genèse par une source de confiance. Cette archive locale de 64 Mio au plus n’est pas une preuve de finalité BFT ni un format d’archives de masse qualifié.
