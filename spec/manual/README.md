# Distribution expérimentale 1.0.0

Cette édition expérimentale finalise la livraison locale 1.0.0. **Le pilote public reste bloqué par la qualification v0.9. Aucune capacité à dix millions n'est annoncée.** Les formats existants, les comptes, les époques et les réseaux restent ceux des jalons précédents. L'éligibilité auto-inscrite convient uniquement au pilote.

## Construire et contrôler

Prérequis de construction : Git pour une copie de travail (facultatif pour une archive source vérifiée), Go ≥ 1.26, Node ≥ 24.12, dépendances npm du verrou et artefacts Semaphore épinglés. Depuis la racine des sources :

```powershell
npm ci --ignore-scripts
npm run artifacts:fetch
./scripts/check.ps1 -Distribution -Report .tools/v1.0-checks.json
$epoch = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
npm run release:build -- --epoch $epoch --out .tools/releases/v1.0
npm run release:finalize -- --release .tools/releases/v1.0 --checks .tools/v1.0-checks.json --output spec/reports/v1.0-distribution.json --allow-test-keys
```

La commande crée des archives **Windows amd64 et Linux amd64**, les sources exactes, le catalogue des schémas avec leurs identifiants et empreintes, l'inventaire des dépendances, la provenance au format SLSA et le manifeste. `--targets linux-arm64` permet une construction supplémentaire. Chaque sortie doit être un nouveau répertoire. Une archive Linux construite sous Windows n'est pas un essai d'exécution Linux.

Les sorties sont dans `payload/`. Chaque archive de plateforme contient sept programmes (`civnode`, `civnet`, `civcrypto`, `civeligibility`, `civqual`, `civrecovery`, `civdist`), la PWA v0.8, ses artefacts publics Semaphore, les auditeurs Node autonomes, les schémas, les profils et ce manuel. Node ≥ 24.12 est requis pour les outils `.mjs`. L'inventaire inclut les dépendances de construction ; il ne certifie pas leur innocuité.

L’archive source contient un inventaire `.civora-source.json` vérifié avant et après copie. Après extraction dans un autre répertoire, installer les dépendances avec `npm ci --ignore-scripts` et les artefacts avec `npm run artifacts:fetch`, puis exécuter les mêmes commandes : aucun dépôt Git n’est nécessaire. Une entrée altérée ou absente est refusée et les fichiers supplémentaires ne sont pas compilés. Pour réutiliser un cache local, définir `CIVORA_ARTIFACT_DIR` ; `CIVORA_GO` peut désigner un exécutable Go installé. Garder les mêmes versions d’outils et les mêmes octets source pour comparer les produits.

Le finaliseur exige le reçu de la suite complète, les mêmes sources et outils, et une archive native strictement identique à celle qui a été exécutée pendant les tests. Modifier le code après ces contrôles impose de les relancer. Il crée un miroir de développement et le vérifie, avec acceptation explicite des clés `TEST_ONLY`. Le rapport distingue **édition expérimentale finalisée** et **pilote public non qualifié**. Il ne publie rien sur Internet et n’active aucun changement de protocole.

Relancer avec les mêmes sources, verrous, outils, cibles et `--epoch` dans un autre répertoire doit produire les mêmes empreintes. `npm run release:compare -- premier/payload second/payload` effectue la comparaison complète. Deux constructions sur cette machine ne constituent pas deux environnements indépendants. La provenance locale est une auto-attestation, sans niveau SLSA revendiqué.

`--public` est refusé avant toute construction. Les données de campagne d'exemple sont réévaluées dans `qualification.json` ; les booléens d'un rapport ne suffisent jamais à ouvrir la publication.

## Vérifier une distribution depuis un miroir

Installer `civdist` et sa racine de confiance par une voie indépendante du miroir. Obtenir également l'empreinte SHA-256 de cette racine par cette voie. Un programme téléchargé depuis une origine compromise ne peut pas établir sa propre confiance. Le vérificateur installé ne dépend ni de la PWA ni d'un consensus disponible.

Pour **un exercice local uniquement**, utiliser le programme natif construit dans `work/` :

```powershell
$dist = '.tools/releases/v1.0/work/civdist.exe'
& $dist seal-test --payload .tools/releases/v1.0/payload --repository .tools/releases/v1.0/manual-mirror --epoch $epoch --allow-test-keys
$root = '.tools/releases/v1.0/manual-mirror/root.TEST_ONLY.json'
$pin = (Get-FileHash -Algorithm SHA256 $root).Hash.ToLowerInvariant()
& $dist verify --repository .tools/releases/v1.0/manual-mirror --root $root --root-sha256 $pin --state .tools/releases/trust/v1.0-manual.db --allow-test-keys
```

Ici le calcul local du pin sert uniquement à l'exercice, puisque nous avons fabriqué la racine. En exploitation, il provient du canal indépendant. Adapter l'epoch à l'heure réelle pour un nouvel exercice : le timestamp expire **24 heures** après cet instant. L'horloge du vérificateur doit être correcte. `seal-test` utilise des graines publiques déterministes `PUBLIC_TEST_ONLY`, ne génère aucune clé d'exploitation et ne peut pas signer une publication publique. Ces signatures n'apportent aucune authenticité face à un attaquant qui connaît ces graines.

Le vérificateur contrôle toute la chaîne TUF et chaque cible, puis affiche un rapport JSON. Il n'extrait, n'exécute et n'installe rien. Après vérification, sélectionner l'archive correspondant au poste, dont le nom, l'empreinte et la taille figurent au rapport. Le fichier miroir est préfixé par cette empreinte. Copier le fichier vérifié hors du miroir avant extraction, contrôler de nouveau son empreinte, puis ouvrir l'archive. Un miroir modifiable peut changer après la vérification.

**Conserver le fichier `--state`** à chaque mise à jour et la même racine initiale épinglée. Le programme persiste les métadonnées acceptées et l'heure observée dans une base transactionnelle verrouillée. Supprimer cette base supprime la mémoire des versions déjà vues. Un retour d'horloge est refusé. Un arrêt brutal libère le verrou système ; relancer avec la même base reprend la vérification. Un disque corrompu exige un diagnostic et une récupération authentifiée, jamais une suppression automatique de l'état.

Racine hors ligne **3/5**, rôle de publication `targets` distinct **3/5**, `snapshot` et `timestamp` distincts **1/1**. Une rotation normale exige les signatures de trois anciennes et trois nouvelles clés. Les métadonnées de développement expirent après un an pour la racine, sept jours pour targets/snapshot et 24 heures pour timestamp. Le vérificateur refuse les clés de test par défaut, les chemins qui sortent du miroir, les versions anciennes et les octets altérés. Les métadonnées TUF authentifient des fichiers ; elles ne prouvent pas la qualité de leur implémentation ni l'indépendance des signataires.

## Utiliser les programmes et les outils d'audit

Depuis la racine du paquet extrait :

```powershell
./bin/civnode.exe -h
./bin/civnet.exe -h
./bin/civcrypto.exe -h
./bin/civrecovery.exe -h
node tools/audit-citizen.mjs -input CHEMIN_DOSSIER -manifest MANIFESTE_AUTHENTIFIE.json
node tools/audit-electionguard.mjs -in ARCHIVE_PUBLIQUE.json
```

Sous Linux, retirer `.exe`. Authentifier le manifeste réseau, la genèse et les points de confiance avant tout audit ; un dossier et son faux manifeste peuvent être cohérents entre eux. L'auditeur citoyen contrôle les preuves natives et le dossier sélectif ; l'auditeur ElectionGuard recalcule les preuves publiques de l'archive. Le rejeu complet du noyau reste disponible dans `civnode`/`civcrypto`. Ces contrôles n'établissent pas à eux seuls la capacité sous charge.

Pour servir la PWA devant un réseau déjà installé, compléter `pwa-networks.example.json` avec un manifeste authentifié et ses origines autorisées, puis :

```powershell
node tools/host-pwa.mjs --config pwa-networks.json --public-dir web --artifact-dir artifacts/semaphore --port 8792
```

Le fichier exemple ne contient aucun réseau. La commande écoute sur `127.0.0.1` et ne démarre aucun validateur. Un accès depuis un autre PC nécessite un hébergement HTTPS correctement configuré. L'atelier de développement avec ses boutons et ses clés publiques se lance depuis les sources avec `npm run dev`; le pont Wi-Fi est décrit dans le README. Aucune donnée locale, clé TLS, base de comptes ou clé VAPID existante n'entre dans les archives.

Dans la PWA : choisir le réseau authentifié ; créer ou importer un coffre ; télécharger la sauvegarde chiffrée et conserver son code séparément ; inscrire le compte ; consulter les scrutins ; exprimer un vote pendant la fenêtre autorisée ; conserver le reçu ; consulter le résultat provisoire puis certifié et exporter le dossier pour l'auditer. Les opérations disponibles dépendent du profil de développement et de l'état du scrutin. Le [README](../../README.md) détaille les fonctions des jalons précédents. Une configuration de reprise que la PWA ne sait pas vérifier impose la lecture seule.

## Mise à jour et incident

Une archive vérifiée ne donne aucune autorisation d'activation. Respecter le plan voté, les empreintes approuvées, les attestations, les barrières, les préavis et les règles d'état du §15.3. Ne remplacer aucun binaire pendant un scrutin en supposant une compatibilité automatique. Répéter le rejeu et la reprise sur une copie authentifiée avant installation ; conserver les exports et les références de l'ancienne version pour l'audit historique.

Si le miroir est compromis, utiliser un autre miroir avec le même vérificateur, la même base et la même racine. Si le seuil de la racine TUF est compromis, sa rotation normale ne rétablit pas la confiance : utiliser la racine de secours indépendante du §15.4 et `civrecovery` avec ses points épinglés. Ne jamais réinitialiser automatiquement la racine à partir du miroir suspect. Le cycle constitutionnel complet et les exercices d'incident restent à qualifier en v0.9.

## Conditions restant avant le pilote public

Il faut terminer le rapport v0.9 avec preuves rejouables, charge réelle sur 32 partitions, mesures WAN/saturation/mobiles, deux audits externes et correction de leurs défauts critiques et élevés. Il faut aussi deux environnements de construction indépendants, deux revues de publication indépendantes, une cérémonie de clés hors ligne 3/5, des miroirs indépendants et l'exercice de compromission/reprise. Publier ensuite la capacité réellement mesurée, les formats figés et les limites du réseau. Ce candidat fournit les outils locaux et conserve ces conditions bloquantes.
