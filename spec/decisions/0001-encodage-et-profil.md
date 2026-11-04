# 0001 — Encodage et périmètre de développement

Statut : adopté pour v0.1, 4 novembre 2026. Références : §§4.1, 12, 13.

Le fichier reçu à la racine est conservé ; sa copie dans `spec/Protocole-Civique-v1.md` constitue le chemin de référence demandé. Aucune règle normative de cette RFC n’est modifiée.

Le noyau accepte uniquement les transactions de compte, le profil `inline_dev` et le mode **development**. Ce mode de genèse complète explicitement la RFC pour les fixtures ; une genèse `pilot` ou `scale_1` est refusée. Les activations préautorisées ne peuvent donc pas être prises pour des décisions d’un réseau public. Aucun simulateur de vote n’est introduit.

Le sous-ensemble JCS accepté exclut tous les nombres JSON : tous les entiers d’application sont des chaînes décimales canoniques `uint64`. Les engagements sont des chaînes non nulles strictement inférieures au module BN254 `21888242871839275222246405745257275088548364400416034343698204186575808495617`. Ce contrôle de champ ne démontre pas la possession d’une identité Semaphore.

Le canoniseur trie les clés selon les unités UTF-16, échappe exactement les contrôles JSON, conserve les caractères non ASCII et n’échappe ni HTML ni U+2028. Il rejette UTF-8 invalide, surrogates isolés, nombres, chaînes non NFC et CR. Les objets de protocole sont constitués de chaînes, booléens, null, tableaux et objets ; aucune conversion implicite n’est permise. Le contrôle NFC/LF s’applique à toutes leurs chaînes, y compris les clés. Les futurs blobs externes devront utiliser leur propre encodage d’octets.

L’entrée doit égaler son réencodage canonique octet pour octet : cela rejette aussi les noms dupliqués, espaces, LF final, BOM et échappements superflus. Les schémas partagés imposent les champs requis, interdisent les champs inconnus à chaque niveau et bornent objets et listes. Les contraintes relationnelles (identifiants, nonces, epoch, module du champ, unicité, autorité) sont vérifiées par le moteur. Les blobs Base64url sont sans padding, avec contrôle des bits résiduels par réencodage strict.

Les limites sont 256 Kio par transaction, 2 Mio par bloc, 8 192 transactions par bloc, 65 536 comptes actifs et 131 072 comptes cumulés. Les limites de longueur des formats de plans sont des choix de développement dans les schémas. Les schémas des actes de gouvernance seront ajoutés avec v0.2 ; les types ultérieurs sont rejetés dès v0.1.

Le vecteur du §13 est conservé sans modification : `v:"1"`, domaine `tx`, identifiant de compte, signature RFC 8032 et racine vide. Le corpus `encoding-negative.json` est commun aux deux implémentations. Les bibliothèques de schéma et de normalisation sont figées par les fichiers de verrouillage.

Référence de canonicalisation : [RFC 8785](https://www.rfc-editor.org/rfc/rfc8785), notamment le tri UTF-16 et les règles d’échappement.
