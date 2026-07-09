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
	"moxiu/r34-dl/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func fetchBulkPosts(client api.Client, query string, want int) ([]api.Post, error) {
	const pageSize = 1000
	var all []api.Post
	page := 0
	for len(all) < want {
		posts, err := client.SearchPosts(query, pageSize, page)
		if err != nil {
			return all, err
		}
		if len(posts) == 0 {
			break
		}
		all = append(all, posts...)
		if len(posts) < pageSize {
			break
		}
		page++
	}
	if len(all) > want {
		all = all[:want]
	}
	return all, nil
}

func main() {
	addKey := flag.Bool("add-api-key", false, "store rule34 api key")
	flag.BoolVar(addKey, "apik", false, "store rule34 api key (short)")

	tags := flag.String("tags", "", "search tags (comma-separated)")
	flag.StringVar(tags, "t", "", "search tags (short)")

	bulk := flag.Bool("bulk", false, "download in bulk")
	flag.BoolVar(bulk, "b", false, "download in bulk (short)")

	limit := flag.Int("limit", 30, "max results to fetch")
	flag.IntVar(limit, "l", 30, "max results to fetch (short)")

	runTests := flag.Bool("run-tests", false, "run tests")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: r34-dl [options]\n\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  -h, --help                       show this help message\n")
		fmt.Fprintf(os.Stderr, "  -apik, --add-api-key             store rule34 api key (prompt's u to paste ur api key)\n")
		fmt.Fprintf(os.Stderr, "  -b, --bulk                       download in bulk\n")
		fmt.Fprintf(os.Stderr, "  -l, --limit <N>                  max results to fetch (default 30)\n")
		fmt.Fprintf(os.Stderr, "  -t, --tags <tags>                search tags (comma-separated)\n")
		fmt.Fprintf(os.Stderr, "  --run-tests                      run tests\n")
	}

	flag.Parse()

	if *runTests {
		sb := api.NewSafebooruClient()
		r34 := api.NewRule34Client("", "")
		cfg := conf.Config{AgeVerified: false, ActiveAPI: "safebooru"}
		p := tea.NewProgram(ui.NewModel(sb, r34, cfg, "", 30))
		if _, err := p.Run(); err != nil {
			fmt.Println("err!", err)
			os.Exit(1)
		}
		return
	}

	if *addKey {
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("paste ur full rule34 API key (e.g. &api_key=xxx&user_id=777): ")
		line, _ := reader.ReadString('\n')
		raw := strings.TrimSpace(line)
		if raw == "" {
			log.Fatal("no creds provided")
		}

		raw = strings.TrimLeft(raw, "&?")
		vals, err := url.ParseQuery(raw)
		if err != nil {
			log.Fatalf("failed to parse credentials: %v", err)
		}

		apiKey := vals.Get("api_key")
		userID := vals.Get("user_id")
		if apiKey == "" || userID == "" {
			log.Fatal("could not find api_key and user_id in the pasted block")
		}

		if err := conf.SaveAPIKey(userID, apiKey); err != nil {
			log.Fatalf("failed to save credentials: %v", err)
		}
		fmt.Printf("saved! user_id=%s api_key=%s***\n", userID, apiKey[:min(8, len(apiKey))])

		fmt.Print("checking API connection... ")
		client := api.NewRule34Client(userID, apiKey)
		if err := client.Ping(); err != nil {
			fmt.Printf("unreachable (%v)\n", err)
		} else {
			fmt.Println("OK!")
		}
		return
	}

	cfg, err := conf.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	sbClient := api.NewSafebooruClient()
	r34Client := api.NewRule34Client(cfg.UserID, cfg.APIKey)

	var activeClient api.Client
	if cfg.AgeVerified && cfg.ActiveAPI == "rule34" {
		activeClient = r34Client
	} else {
		activeClient = sbClient
	}

	if *bulk {
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
			log.Fatalf("search failed: %v", err)
		}
		if len(posts) == 0 {
			fmt.Println("no results")
			return
		}
		fmt.Printf("bulk downloading %d posts...\n", len(posts))
		d := dl.New("downloads", 4)
		done, failed := 0, 0
		for r := range d.DownloadAll(posts) {
			if r.Err != nil {
				failed++
				fmt.Printf("failed #%d: %v\n", r.Post.ID, r.Err)
				continue
			}
			done++
			fmt.Printf("saved #%d -> %s\n", r.Post.ID, r.Path)
		}
		fmt.Printf("done: %d saved, %d failed\n", done, failed)
		return
	}

	p := tea.NewProgram(ui.NewModel(sbClient, r34Client, cfg, *tags, *limit))
	if _, err := p.Run(); err != nil {
		fmt.Println("err!", err)
		os.Exit(1)
	}
}
