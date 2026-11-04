# 0002 — État et cycle de vie des comptes

Statut : adopté pour v0.1. Références : §§4.2, 5.

Les valeurs d’état sont des documents JCS. `accounts[account_id]` contient les clés courantes, l’engagement, le prochain nonce, le statut, la hauteur d’inscription et les réservations historiques `used_keys`, `used_commitments`. Les deux listes suivent l’ordre de première utilisation et ne contiennent pas de doublons. Chaque liste est bornée à 262 144 éléments ; une transition qui dépasse cette limite échoue avec `ERR_CAPACITY`.

La RFC réserve définitivement les engagements utilisés. Pour les clés, elle ne précise pas leur réattribution après retrait ou rotation. v0.1 adopte une règle conservatrice : toute clé déjà liée à un compte reste réservée à ce compte, dans les deux rôles. Un même compte peut réutiliser ses propres clés, mais les clés d’usage et de récupération courantes doivent toujours être distinctes. Un engagement historique remplacé ne peut pas redevenir courant ; conserver l’engagement actuel pendant une rotation de clés est permis. Modifier ces règles exige de changer le format de développement et ses vecteurs.

La signature d’inscription est vérifiée avec la nouvelle clé d’usage. La rotation et le retrait utilisent exclusivement la clé de récupération actuellement enregistrée. L’identifiant du compte ne change pas. Le retrait est terminal et conserve les réservations. Les engagements d’un ancien scrutin resteront du ressort de son futur adaptateur d’instantané.

`network["config"]` contient `{config,height,time_ms}`. La hauteur et le maximum du temps BFT sont engagés dans la racine, car ils conditionnent l’exécution future. `upgrades[plan_id]` contient le plan, son instance et l’état `armed` ou `activated`. Aucun index local ou événement n’est ajouté à l’arbre. Les autres tables de la RFC seront introduites à leurs jalons.

La clé de feuille est exactement `D("state/key",[table,key])` avec clé métier chaîne pour ces trois tables. L’arbre de profondeur 256 descend par les bits de poids fort. Les feuilles et parents suivent les préfixes 0x00, 0x01, 0x02 du §4.2. Le calcul reconstruit l’arbre depuis les entrées : cette implémentation privilégie la vérifiabilité locale, sans qualification de capacité à grande échelle.

Les blocs sont contigus à partir de 1 ; le temps est `max(previous,header_ms)`. Les transactions s’exécutent dans l’ordre reçu sur une copie candidate. Tout échec invalide l’ensemble du bloc et abandonne ses nonces, écritures et événements. `CheckTx` valide sur une copie, sans réserver de nonce. Il ne prédit pas l’ordre final. Le nonce maximal uint64 est refusé avant incrément ; aucun débordement n’est permis.

L’index des événements est une chaîne décimale commençant à zéro. Les événements d’activation précèdent ceux des transactions. Chaque événement est identifié par `D("event",[network_id,height,index])`. Le profil v0.1 ne comporte pas encore d’autres transitions échéantes.
