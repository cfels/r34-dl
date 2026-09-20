package api

const (
	bulkPageSize = 1000
	bulkMaxPages = 20
)

func FetchPosts(client Client, query string, want int) ([]Post, error) {
	var all []Post
	seen := make(map[int]bool)
	for page := 0; page < bulkMaxPages && len(all) < want; page++ {
		posts, err := client.SearchPosts(query, bulkPageSize, page)
		if err != nil {
			return all, err
		}
		if len(posts) == 0 {
			break
		}
		added := 0
		for _, post := range posts {
			if post.ID != 0 {
				if seen[post.ID] {
					continue
				}
				seen[post.ID] = true
			}
			all = append(all, post)
			added++
		}
		if added == 0 {
			break
		}
	}
	if len(all) > want {
		all = all[:want]
	}
	return all, nil
}
