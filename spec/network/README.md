# Participer au réseau local

L’interface propose deux espaces : **Application** pour les propositions, les votes, les lois, les rôles et le compte ; **Réseau** pour l’activité des nœuds, les téléchargements, les vérifications et les commandes de l’atelier. Un seul réseau est proposé. L’archive historique v0.6 n’est plus démarrée ni exposée par le lanceur ; ses fixtures sont conservées pour la régression du protocole.

## Vérifier le réseau en une commande

1. Ouvrir **Réseau → Vérifier le réseau** sur l’ordinateur qui observera le registre.
2. Télécharger l’archive Windows x64 ou Linux x64 et l’extraire entièrement dans un nouveau dossier.
3. Ouvrir un terminal dans ce dossier et exécuter :

   Windows / PowerShell :
   ```powershell
   .\civora-reseau.exe
   ```

   Linux :
   ```sh
   chmod +x civora-reseau && ./civora-reseau
   ```

Go et Node.js ne sont pas nécessaires sur ce poste. Le programme télécharge les blocs par HTTP(S), vérifie leurs signatures, leurs liens et leurs transitions d’état avec le même vérificateur que `civnode`, depuis la genèse incluse. Il conserve les blocs et son identité dans `donnees/`. Aucun port entrant n’est ouvert et aucun compte civique n’est nécessaire. `Ctrl+C` arrête l’observateur ; relancer la même commande reprend son archive après un rejeu vérifié.

Pour un autre PC, ouvrir la page depuis **l’adresse HTTPS Wi-Fi du serveur**, pas depuis `127.0.0.1`, puis télécharger depuis cette origine. Le pont Wi-Fi transmet son origine au générateur d’archives : le bootstrap contient ainsi l’adresse utilisable par le destinataire. La CA locale publique est incluse dans `reseau.json` si nécessaire ; elle n’est pas installée dans le système. Le client ajoute cette CA à son propre transport TLS et vérifie le certificat et son nom. Il n’existe aucun mode TLS sans vérification.

Les archives et leur empreinte SHA-256 sont servies sous `/network/download/windows-amd64.zip` ou `/network/download/linux-amd64.zip`, et les métadonnées d’empreinte sous le même chemin avec `.sha256`. Les quatre entrées sont l’exécutable, `reseau.json`, `LIRE-MOI.txt` et `SHA256SUMS`. Aucune clé privée n’y figure. La clé privée de présence est créée sur le poste au premier lancement : ne pas partager `donnees/`.

## Participer au consensus : maquette uniquement

La page **Réseau → Participer au consensus** présente un aperçu visuel d’un futur parcours de validateur. Elle n’effectue aucune inscription, génération de clés, installation ni activation. Son bouton de participation est désactivé. Les étapes affichées sont indicatives et les règles d’admission restent à définir.

La page de l’observateur utilise `#/network/observer`. Les anciens liens `#/network/listen` et `#/network/join` continuent d’ouvrir ce même parcours d’observation ; ils n’activent pas de rôle de validateur.

## Explorer les blocs et les états

**Explorateur → Blocs** affiche dix blocs par page, avec accès aux blocs plus anciens et recherche par hauteur. Le détail affiche la date, les empreintes, les signatures du commit et les données publiques des vingt premières transactions. L’en-tête et la racine de toutes les transactions sont vérifiés dans le navigateur, ainsi que les liens entre les blocs d’une même page. Ce contrôle ne prétend pas vérifier l’exécution complète ni les preuves d’évidence du bloc ; l’observateur effectue le rejeu du registre.

**Explorateur → États du registre** consulte la configuration du réseau ou une clé publique de compte, proposition, scrutin, loi ou rôle, à la hauteur choisie. Le résultat vérifie la preuve de présence ou d’absence et peut être téléchargé. L’état après le bloc H est authentifié par l’en-tête signé du bloc H+1 ; la racine `app_hash` d’un en-tête de bloc décrit donc l’état avant ce bloc. Les coffres, sauvegardes et choix de vote locaux ne sont pas consultés.

**Vérifier un reçu** est réservé à l’import et au contrôle d’un reçu de vote. Son adresse est `#/network/receipt` ; les anciennes adresses `#/network/verify` et `#/verify` restent des alias de ce contrôle.

## Comprendre les compteurs

- **Nœuds actifs** : le serveur de l’atelier et les identités d’observateurs ayant annoncé leur présence depuis moins de 45 secondes.
- **Dont observateurs** : ces identités d’observateurs uniquement. Une relance dans le même dossier conserve la clé et n’ajoute pas de doublon. Un second dossier crée une seconde identité ; il ne prouve pas l’existence d’une seconde machine.
- **Comptes citoyens actifs** : comptes actifs annoncés par l’index du registre, hors comptes de démonstration désignés par la politique des gardiens de la genèse. Ce compteur ne prétend pas mesurer des personnes uniques.

La présence est signée avec une clé locale Ed25519 dédiée. Le serveur émet un défi à usage unique valable 30 secondes, contrôle la signature, le réseau et l’empreinte d’un bloc connu. Une annonce est envoyée toutes les 10 secondes. Le client peut annoncer son départ ; après un arrêt brutal, le bail expire sous 45 secondes. Le tableau s’actualise toutes les 15 secondes, uniquement dans l’espace Réseau. Les compteurs de présence sont locaux au serveur qui les reçoit et sont réinitialisés à son redémarrage, puis reconstitués par les annonces. Ils ne constituent ni une preuve de décentralisation, ni un système résistant aux identités multiples.

## Exploitation de l’atelier

`npm run dev` construit les deux exécutables d’observation puis lance l’application. Les builds sont mis en cache sous `.tools/network/participants/` avec les empreintes des sources Go, des schémas et du compilateur. Le téléchargement incorpore ensuite la genèse et l’origine du serveur courant ; il est reproductible pour les mêmes entrées. Les fichiers de génération restent sous `.tools/`.

Le nouveau lancement d’un atelier crée un nouveau réseau. Une archive déjà téléchargée reste liée à l’ancien réseau. Le client refuse une autre genèse, un autre bootstrap dans son dossier existant, un bloc invalide ou un recul de hauteur ; il ne supprime aucune archive pour contourner un refus. Pour un nouvel atelier, télécharger sa nouvelle archive et utiliser un autre dossier.

Les observateurs ne votent pas au consensus et ne sont pas des validateurs. Le serveur d’atelier fabrique des commits synthétiques avec des clés de test publiques ; rejoindre cet atelier ne qualifie pas un réseau public. Les téléchargements locaux ne sont pas une nouvelle publication TUF signée de la distribution 1.0.0. Comparer la genèse et l’empreinte de l’archive avec l’organisateur par un canal indépendant avant d’exécuter un binaire reçu.

Validation ciblée : `npm run test:network` ; tests Go de rejeu et d’arrêt brutal : `go test ./internal/participation`. Les tests de navigateur utilisent Chromium et WebKit en émulation mobile ; ils ne qualifient pas des appareils physiques.
