package backend

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// attachment is a file attached to an issue or comment, as its Markdown
// names it: an image you pasted, or a screenshot a bot posted.
var attachment = regexp.MustCompile(`^https://github\.com/user-attachments/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// signedImage is where the rendered HTML of a private repository's comment
// says an attachment is: a link that works without credentials for a few
// minutes, with the attachment's id in its path.
var signedImage = regexp.MustCompile(`https://private-user-images\.githubusercontent\.com/[^"'\s<>]+`)

// renderedQuery is an issue's or pull request's body and comments as GitHub
// renders them, which is where the signed links are.
const renderedQuery = `query Rendered($owner: String!, $name: String!, $number: Int!) { repository(owner: $owner, name: $name) {
	item: issueOrPullRequest(number: $number) {
		... on Issue { bodyHTML comments(first: 100) { nodes { bodyHTML } } }
		... on PullRequest { bodyHTML comments(first: 100) { nodes { bodyHTML } } } } } }`

// maxImage is the largest image the app shows.
const maxImage = 50 << 20

// images are the attachments already fetched, by id, as data: URLs. An
// attachment never changes, so each is fetched once.
var images = struct {
	sync.Mutex
	byID map[string]string
}{byID: map[string]string{}}

// Image is an image attached to issue or pull request number of repo, by
// the URL its Markdown gives, as a data: URL the window can show. In a
// private repository that URL needs GitHub's credentials, which the window
// does not have, so the app fetches the image itself, from the signed link
// in GitHub's rendering of the conversation.
func (g *GitHub) Image(repo string, number int, src string) (string, error) {
	m := attachment.FindStringSubmatch(src)
	if m == nil {
		return "", fmt.Errorf("%s is not a file attached on GitHub", src)
	}
	id := strings.ToLower(m[1])
	images.Lock()
	cached := images.byID[id]
	images.Unlock()
	if cached != "" {
		return cached, nil
	}

	vars, err := repoVars(repo)
	if err != nil {
		return "", err
	}
	vars["number"] = number
	var v struct {
		Repository *struct {
			Item *struct {
				BodyHTML string `json:"bodyHTML"`
				Comments struct {
					Nodes []struct {
						BodyHTML string `json:"bodyHTML"`
					} `json:"nodes"`
				} `json:"comments"`
			} `json:"item"`
		} `json:"repository"`
	}
	if err := g.graphql(renderedQuery, vars, &v); err != nil {
		return "", err
	}
	if v.Repository == nil || v.Repository.Item == nil {
		return "", fmt.Errorf("%s has no #%d", repo, number)
	}
	rendered := []string{v.Repository.Item.BodyHTML}
	for _, c := range v.Repository.Item.Comments.Nodes {
		rendered = append(rendered, c.BodyHTML)
	}
	signed := ""
	for _, u := range signedImage.FindAllString(strings.Join(rendered, "\n"), -1) {
		u = html.UnescapeString(u)
		if path, _, _ := strings.Cut(u, "?"); strings.Contains(strings.ToLower(path), id) {
			signed = u
			break
		}
	}
	if signed == "" {
		// A public repository's attachment needs no credentials.
		signed = src
	}

	data, err := fetchImage(signed)
	if err != nil {
		return "", err
	}
	images.Lock()
	images.byID[id] = data
	images.Unlock()
	return data, nil
}

// fetchImage downloads an image, as a data: URL.
func fetchImage(url string) (string, error) {
	resp, err := httpClient(time.Minute).Get(url)
	if err != nil {
		return "", errors.New("cannot reach GitHub: is this computer online?")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s for the image", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxImage {
		return "", errors.New("the image is too big to show")
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "image/") {
		ct = http.DetectContentType(body)
	}
	if !strings.HasPrefix(ct, "image/") {
		return "", fmt.Errorf("GitHub gave %s, not an image", ct)
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(body), nil
}
