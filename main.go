package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const (
	appName = "imapstats"

	defaultDirPerms = 0700
)

var (
	addrArg = flag.String("addr", "imap.gmail.com:993", "IMAP user")

	userArg     = flag.String("user", "", "IMAP user")
	passwordArg = flag.String("pass", "", "IMAP password")

	mboxArg = flag.String("mailbox", "INBOX", "mailbox on the server")

	writeCacheArg = flag.Bool("write-cache", false, "if true writes to cache")
	readCacheArg  = flag.Bool("read-cache", false, "if true reads from cache")

	appHomeDir string
	cacheDir   string
)

type stats struct {
	UnseenCount int `json:"unseen_count"`
}

func init() {
	log.SetFlags(0)
	flag.Parse()

	must(initPaths())
}

func initPaths() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	appHomeDir = filepath.Join(homeDir, "."+appName)
	cacheDir = filepath.Join(appHomeDir, "cache")

	for _, dir := range []string{appHomeDir, cacheDir} {
		if err := os.MkdirAll(dir, defaultDirPerms); err != nil {
			return err
		}
	}
	return nil
}

func fetchStats(c *client.Client) (*stats, error) {
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(criteria)
	if err != nil {
		return nil, err
	}
	return &stats{UnseenCount: len(ids)}, nil
}

func main() {
	if *readCacheArg {
		must(readFromCache())
		return
	}
	// Connect to server
	c, err := client.DialTLS(*addrArg, nil)
	dieIf(err)
	log.Println("Connected")

	// Don't forget to logout
	defer c.Logout()

	// Login
	password, err := getPassword()
	dieIf(err)

	must(c.Login(*userArg, password))
	log.Println("Logged in")

	_, err = c.Select(*mboxArg, false)
	dieIf(err)

	st, err := fetchStats(c)
	dieIf(err)

	must(writeStats(st))
}

func readFromCache() error {
	f, err := os.Open(cacheFilename())
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = io.Copy(os.Stdout, f)
	return err
}

func writeStats(st *stats) error {
	var w io.Writer = os.Stdout
	if *writeCacheArg {
		f, err := os.Create(cacheFilename())
		if err != nil {
			return err
		}
		defer f.Close()
		w = io.MultiWriter(w, f)
	}
	return json.NewEncoder(w).Encode(st)
}

func cacheFilename() string {
	return filepath.Join(cacheDir, *userArg+"."+*mboxArg)
}

func getPassword() (string, error) {
	return *passwordArg, nil
}

func dieIf(err error) {
	if err != nil {
		log.Fatal("fatal:", err)
	}
}

func must(err error) {
	dieIf(err)
}
