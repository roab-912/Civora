package participation

import (
	"archive/zip"
	"civora/internal/archive"
	"flag"
	"fmt"
	"os"
	"time"
)

// Bundle packages only an explicitly built executable and public bootstrap.
// No workspace directory, local identity, account, or TLS private key is copied.
func Bundle(args []string) error {
	f := flag.NewFlagSet("bundle", flag.ContinueOnError)
	binary := f.String("binary", "", "native observer executable")
	bootstrap := f.String("config", "", "public bootstrap file")
	output := f.String("output", "", "new ZIP output")
	target := f.String("target", "", "windows-amd64 or linux-amd64")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || (*target != "windows-amd64" && *target != "linux-amd64") {
		return fmt.Errorf("unsupported observer target")
	}
	configRaw, err := os.ReadFile(*bootstrap)
	if err != nil {
		return err
	}
	config, _, err := DecodeConfig(configRaw)
	if err != nil {
		return err
	}
	executable, err := os.ReadFile(*binary)
	if err != nil {
		return err
	}
	name, command := "civora-reseau", "chmod +x civora-reseau && ./civora-reseau"
	if *target == "windows-amd64" {
		name, command = "civora-reseau.exe", ".\\civora-reseau.exe"
	}
	readme := []byte(fmt.Sprintf("CIVORA — REJOINDRE LE RÉSEAU LOCAL EXPÉRIMENTAL\n\n1. Extraire toute cette archive dans un dossier.\n2. Ouvrir un terminal dans ce dossier et exécuter :\n\n%s\n\nAucune installation de Go ou Node.js. Le programme vérifie les blocs et garde une copie du registre dans donnees/. Laisser le terminal ouvert ; Ctrl+C arrête le nœud. Relancer la même commande reprend l'archive et conserve l'identité. Ne pas partager le dossier donnees/ : il contient une clé privée de présence générée au premier démarrage.\n\nRéseau : %s\nGenèse SHA-256 : %s\nSource : %s\n\nL'archive est liée à CET atelier. Si le serveur recrée un réseau, télécharger sa nouvelle archive et utiliser un nouveau dossier. Ne jamais effacer automatiquement les données d'un ancien réseau. Pour un autre PC, télécharger depuis l'adresse Wi-Fi HTTPS du serveur, pas depuis 127.0.0.1. Une CA locale, si nécessaire, est incluse dans reseau.json ; elle n'est pas installée dans le système. TLS vérifie toujours le certificat et son nom.\n\nRôle : observateur HTTP. Les signatures, liens des blocs et transitions d'état sont vérifiés localement depuis la genèse incluse. Aucun compte civique ni droit de validation n'est créé. Cet atelier utilise des commits synthétiques et des clés de consensus PUBLIC_TEST_ONLY. La présence déclarée est annoncée toutes les 10 secondes et expire après 45 secondes sans nouvelles ; ce compteur ne prouve ni le nombre de personnes, ni l'indépendance des machines. Aucun port entrant ni ouverture de pare-feu n'est nécessaire.\n\nComparer l'empreinte de l'archive et de la genèse avec l'organisateur par un canal de confiance. Les sommes SHA-256 du même téléchargement ne constituent pas une certification externe.\n", command, config.NetworkID, config.GenesisSHA256, config.API))
	files := []struct {
		name string
		raw  []byte
		mode os.FileMode
	}{{name, executable, 0755}, {"reseau.json", configRaw, 0644}, {"LIRE-MOI.txt", readme, 0644}}
	sums := ""
	for _, file := range files {
		sums += archive.Digest(file.raw) + "  " + file.name + "\n"
	}
	files = append(files, struct {
		name string
		raw  []byte
		mode os.FileMode
	}{"SHA256SUMS", []byte(sums), 0644})
	out, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	z := zip.NewWriter(out)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(file.mode)
		w, createErr := z.CreateHeader(header)
		if createErr != nil {
			_ = z.Close()
			return createErr
		}
		if _, err = w.Write(file.raw); err != nil {
			_ = z.Close()
			return err
		}
	}
	if err = z.Close(); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	return nil
}
