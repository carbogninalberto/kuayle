package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/service"
)

const backupUsage = `Usage: server backup decrypt <input.tar.gz.enc> <output.tar.gz>

Decrypts a backup written with client-side encryption. The passphrase is read
from KUAYLE_BACKUP_PASSPHRASE or, when that is unset, from the first line of
standard input. It never appears on the command line. See docs/backups.md.`

func runBackupCommand(args []string) {
	if err := backupCommand(args, os.Stdin, os.Getenv("KUAYLE_BACKUP_PASSPHRASE")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func backupCommand(args []string, stdin io.Reader, envPassphrase string) error {
	if len(args) != 3 || args[0] != "decrypt" {
		return fmt.Errorf("%s", backupUsage)
	}
	passphrase := envPassphrase
	if passphrase == "" {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("no passphrase: set KUAYLE_BACKUP_PASSPHRASE or pass it on standard input")
		}
		passphrase = strings.TrimRight(line, "\r\n")
	}
	in, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer in.Close()
	outPath := args[2]
	partial := outPath + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := service.DecryptBackup(out, bufio.NewReaderSize(in, 1<<20), passphrase); err != nil {
		out.Close()
		os.Remove(partial)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(partial)
		return err
	}
	return os.Rename(partial, outPath)
}
