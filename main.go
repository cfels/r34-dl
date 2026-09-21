package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/dl"
	"moxiu/r34-dl/safe"
	"moxiu/r34-dl/ui"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

func readSecretLine(r *os.File) string {
	if term.IsTerminal(int(r.Fd())) {
		if data, err := term.ReadPassword(int(r.Fd())); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	reader := bufio.NewReader(r)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func failureLine(id int, err error) string {
	return fmt.Sprintf("failed #%d: %s", id, safe.Text(err.Error()))
}

func fetchBulkPosts(client api.Client, query string, want int) ([]api.Post, error) {
	return api.FetchPosts(client, query, want)
}

func main() {
	addKey := flag.Bool("add-api-key", false, "store rule34 api key")
	flag.BoolVar(addKey, "apik", false, "store rule34 api key (short)")

	tags := flag.String("tags", "", "search tags (comma-separated)")
	flag.StringVar(tags, "t", "", "search tags (short)")

	bulk := flag.Bool("bulk", false, "bulk download mode (asks how many posts to grab)")
	flag.BoolVar(bulk, "b", false, "bulk download mode (short)")

	limit := flag.Int("limit", 30, "max results to fetch")
	flag.IntVar(limit, "l", 30, "max results to fetch (short)")

	clearHistory := flag.Bool("clear-history", false, "clear search history")
	flag.BoolVar(clearHistory, "cls", false, "clear search history (short)")

	site := flag.String("api", "", "site to use ("+strings.Join(api.SiteNames(), ", ")+")")

	audioOn := flag.Bool("audio", false, "play video audio (default off)")
	flag.BoolVar(audioOn, "a", false, "play video audio (short)")
	audioOff := flag.Bool("no-audio", false, "mute video audio")

	filterAI := flag.Bool("filter-ai", false, "hide AI generated posts")
	noFilterAI := flag.Bool("no-filter-ai", false, "show AI generated posts")

	blacklist := flag.String("blacklist", "", "hide tags from results and store them in config (comma-separated)")
	flag.StringVar(blacklist, "bl", "", "hide tags from results and store them in config (short)")
	clearBlacklist := flag.Bool("blacklist-clear", false, "clear the tag blacklist")
	flag.BoolVar(clearBlacklist, "blc", false, "clear the tag blacklist (short)")

	runTests := flag.Bool("run-tests", false, "run tests")

	showVersion := flag.Bool("version", false, "print version and commit")
	flag.BoolVar(showVersion, "v", false, "print version and commit (short)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: r34-dl [options]\n\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  -h, --help                       show this help message\n")
		fmt.Fprintf(os.Stderr, "  -v, --version                    print version and commit\n")
		fmt.Fprintf(os.Stderr, "  -apik, --add-api-key             store rule34 api key (prompt's u to paste ur api key)\n")
		fmt.Fprintf(os.Stderr, "  -b, --bulk                       bulk download mode (asks how many posts to grab)\n")
		fmt.Fprintf(os.Stderr, "  -l, --limit <N>                  max results to fetch (default 30)\n")
		fmt.Fprintf(os.Stderr, "  -t, --tags <tags>                search tags (comma-separated)\n")
		fmt.Fprintf(os.Stderr, "  --api <site>                     site to use (safebooru, rule34, pornhub, xvideos, xhamster)\n")
		fmt.Fprintf(os.Stderr, "  -cls, --clear-history            clear search history\n")
		fmt.Fprintf(os.Stderr, "  -a, --audio                      play video audio (default: off)\n")
		fmt.Fprintf(os.Stderr, "  --no-audio                       mute video audio\n")
		fmt.Fprintf(os.Stderr, "  --filter-ai                      hide AI generated posts\n")
		fmt.Fprintf(os.Stderr, "  --no-filter-ai                   show AI generated posts\n")
		fmt.Fprintf(os.Stderr, "  -bl, --blacklist <tags>          hide tags from results and store them in config (comma-separated, prefix a tag with - to remove it)\n")
		fmt.Fprintf(os.Stderr, "  -blc, --blacklist-clear          clear the tag blacklist\n")
		fmt.Fprintf(os.Stderr, "  --run-tests                      run tests\n")
	}

	flag.Parse()

	if *showVersion {
		fmt.Println(ui.VersionBanner())
		return
	}

	if *audioOn || *audioOff {
		cfg, err := conf.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		cfg.AudioEnabled = *audioOn
		if err := conf.Save(cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
		if cfg.AudioEnabled {
			fmt.Println("video audio enabled (press m while a video plays to toggle)")
		} else {
			fmt.Println("video audio disabled")
		}
		return
	}

	if *filterAI || *noFilterAI {
		cfg, err := conf.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		cfg.FilterAI = *filterAI
		if err := conf.Save(cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
		if cfg.FilterAI {
			fmt.Println("AI generated posts will be filtered out (toggle with ctrl+a while searching)")
		} else {
			fmt.Println("AI generated posts are shown again")
		}
		return
	}

	if *clearHistory {
		cfg, err := conf.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		cfg.SearchHistory = nil
		if err := conf.Save(cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
		fmt.Println("search history cleared")
		return
	}

	if *clearBlacklist {
		cfg, err := conf.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		cfg.Blacklist = nil
		if err := conf.Save(cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
		fmt.Println("tag blacklist cleared")
		return
	}

	if *blacklist != "" {
		cfg, err := conf.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		cfg, added, removed := conf.AddBlacklist(cfg, *blacklist)
		if err := conf.Save(cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
		if len(added) > 0 {
			fmt.Printf("blacklisted: %s\n", strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			fmt.Printf("removed from blacklist: %s\n", strings.Join(removed, ", "))
		}
		if len(added) == 0 && len(removed) == 0 {
			fmt.Println("blacklist unchanged")
		}
		if len(cfg.Blacklist) > 0 {
			fmt.Printf("blacklist now: %s\n", strings.Join(cfg.Blacklist, ", "))
		} else {
			fmt.Println("blacklist is empty")
		}
		return
	}

	if *runTests {
		os.Exit(runTestSuites(flag.Args()))
	}

	if *addKey {
		fmt.Print("paste ur full rule34 API key (e.g. &api_key=xxx&user_id=777): ")
		raw := readSecretLine(os.Stdin)
		fmt.Println()
		if raw == "" {
			log.Fatal("no creds provided")
		}

		raw = strings.TrimLeft(raw, "&?")
		vals, err := url.ParseQuery(raw)
		if err != nil {
			log.Fatal("couldn't parse the pasted block, expected something like &api_key=xxx&user_id=777")
		}

		apiKey := vals.Get("api_key")
		userID := vals.Get("user_id")
		if apiKey == "" || userID == "" {
			log.Fatal("could not find api_key and user_id in the pasted block")
		}

		if err := conf.SaveAPIKey(userID, apiKey); err != nil {
			log.Fatalf("failed to save credentials: %v", err)
		}
		fmt.Printf("saved! user_id=%s api_key=stored (%d chars)\n", safe.Tag(userID), len(apiKey))

		fmt.Print("checking API connection... ")
		client := api.NewRule34Client(userID, apiKey)
		if err := client.Ping(); err != nil {
			fmt.Printf("unreachable (%s)\n", safe.Text(err.Error()))
		} else {
			fmt.Println("OK!")
		}
		return
	}

	cfg, err := conf.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if *site != "" {
		name := strings.ToLower(strings.TrimSpace(*site))
		if !api.KnownSite(name) {
			log.Fatalf("unknown site %q, choose from: %s", *site, strings.Join(api.SiteNames(), ", "))
		}
		if api.IsAdultSite(name) && !cfg.AgeVerified {
			log.Fatalf("%s is age restricted, run r34-dl once and answer the age prompt first", name)
		}
		cfg.ActiveAPI = name
		if err := conf.Save(cfg); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
	}

	clients := map[string]api.Client{
		"safebooru": api.NewSafebooruClient(),
		"rule34":    api.NewRule34Client(cfg.UserID, cfg.APIKey),
		"pornhub":   api.NewPornHubClient(),
		"xvideos":   api.NewXVideosClient(),
		"xhamster":  api.NewXHamsterClient(),
	}

	activeClient := clients[cfg.ActiveAPI]
	if !cfg.AgeVerified || activeClient == nil {
		activeClient = clients["safebooru"]
	}

	if *bulk && !term.IsTerminal(int(os.Stdin.Fd())) {
		reader := bufio.NewReader(os.Stdin)

		query := *tags
		if query == "" {
			fmt.Print("Enter search tags: ")
			line, _ := reader.ReadString('\n')
			query = strings.TrimSpace(line)
		}

		count := *limit
		fmt.Printf("how many posts would you like to download? (default %d): ", *limit)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			n, err := strconv.Atoi(line)
			if err != nil || n <= 0 {
				fmt.Printf("invalid number %q, using default %d\n", line, *limit)
			} else {
				count = n
			}
		}

		posts, err := fetchBulkPosts(activeClient, query, count)
		if err != nil {
			log.Fatalf("search failed: %s", safe.Text(err.Error()))
		}
		if len(posts) == 0 {
			fmt.Println("no results")
			return
		}
		fmt.Printf("bulk downloading %d posts...\n", len(posts))
		d := dl.New("r34-dl_downloads", 4)
		done, failed := 0, 0
		for r := range d.DownloadAll(posts) {
			if r.Err != nil {
				failed++
				fmt.Println(failureLine(r.Post.ID, r.Err))
				continue
			}
			done++
			fmt.Printf("saved #%d -> %s\n", r.Post.ID, safe.Text(r.Path))
		}
		fmt.Printf("done: %d saved, %d failed\n", done, failed)
		return
	}

	p := tea.NewProgram(ui.NewModel(clients, cfg, *tags, *limit).WithBulk(*bulk))
	if _, err := p.Run(); err != nil {
		fmt.Println("err!", err)
		os.Exit(1)
	}
}
