package profile_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestSearchUsersMinLength(t *testing.T) {
	srv := testutil.NewServer(t)
	database := testutil.Database(t, srv)
	ada := testutil.CreateUser(t, database, testutil.WithName("Ada Lovelace"), testutil.WithEmail("ada@corp.test"))
	accented := testutil.CreateUser(t, database, testutil.WithName("éééé Person"), testutil.WithEmail("accent@corp.test"))
	client := testutil.LoginAs(t, srv, ada)

	for _, q := range []string{"", "a", "ad", "ada", "Ada", "lov", "ééé"} {
		status, hits := getSearch(t, client, srv.URL, q)
		if status != http.StatusOK || len(hits) != 0 {
			t.Fatalf("q %q: status %d hits %d", q, status, len(hits))
		}
	}

	res, err := client.Get(srv.URL + "/api/users/search")
	if err != nil {
		t.Fatal(err)
	}
	status, hits := readHits(t, res)
	if status != http.StatusOK || len(hits) != 0 {
		t.Fatalf("missing q: status %d hits %d", status, len(hits))
	}

	status, hits = getSearch(t, client, srv.URL, "éééé")
	if status != http.StatusOK || len(hits) != 1 || hitID(t, hits[0]) != accented.ID {
		t.Fatalf("rune boundary: status %d hits %#v", status, hits)
	}
}

func TestSearchUsersPrefix(t *testing.T) {
	srv := testutil.NewServer(t)
	database := testutil.Database(t, srv)
	ada := testutil.CreateUser(t, database,
		testutil.WithName("Ada Lovelace"),
		testutil.WithEmail("ada.lovelace@corp.test"),
		testutil.WithAvatarPath("avatar/ada.jpg"),
	)
	adam := testutil.CreateUser(t, database, testutil.WithName("Adam West"), testutil.WithEmail("adam.west@corp.test"))
	mary := testutil.CreateUser(t, database, testutil.WithName("Mary Ann Evans"), testutil.WithEmail("mary.ann@corp.test"))
	spaced := testutil.CreateUser(t, database, testutil.WithName("Ann  Marie"), testutil.WithEmail("ann.marie@corp.test"))
	beatrice := testutil.CreateUser(t, database, testutil.WithName("Beatrice"), testutil.WithEmail("beatrice@corp.test"))
	me := testutil.CreateUser(t, database, testutil.WithName("Quinn Query"), testutil.WithEmail("quinn@corp.test"))
	client := testutil.LoginAs(t, srv, me)

	cases := []struct {
		q    string
		want []int64
	}{
		{q: "love", want: []int64{ada.ID}},
		{q: "LOVE", want: []int64{ada.ID}},
		{q: "ada.", want: []int64{ada.ID}},
		{q: "Ada ", want: []int64{ada.ID}},
		{q: "adam", want: []int64{adam.ID}},
		{q: "mary", want: []int64{mary.ID}},
		// "ann " is a prefix of "Ann  Marie" and a whole word in "Mary Ann Evans".
		{q: "ann ", want: []int64{spaced.ID, mary.ID}},
		{q: "evan", want: []int64{mary.ID}},
		{q: "marie", want: []int64{spaced.ID}},
		{q: "beat", want: []int64{beatrice.ID}},
		{q: "bea ", want: nil},
		{q: "ovel", want: nil},
		{q: "corp", want: nil},
		{q: "ada'", want: nil},
	}
	for _, tt := range cases {
		t.Run(tt.q, func(t *testing.T) {
			status, hits := getSearch(t, client, srv.URL, tt.q)
			if status != http.StatusOK {
				t.Fatalf("status %d", status)
			}
			assertHitIDs(t, hits, tt.want)
		})
	}

	status, hits := getSearch(t, client, srv.URL, "love")
	if status != http.StatusOK || len(hits) != 1 {
		t.Fatalf("ada hit %d %#v", status, hits)
	}
	assertHit(t, hits[0], ada, "/uploads/avatar/ada.jpg")
	status, hits = getSearch(t, client, srv.URL, "adam")
	if status != http.StatusOK || len(hits) != 1 {
		t.Fatalf("adam hit %d %#v", status, hits)
	}
	assertHit(t, hits[0], adam, nil)
}

func TestSearchUsersEscaping(t *testing.T) {
	srv := testutil.NewServer(t)
	database := testutil.Database(t, srv)
	percent := testutil.CreateUser(t, database, testutil.WithName("Percent"), testutil.WithEmail("%abc@corp.test"))
	percentWild := testutil.CreateUser(t, database, testutil.WithName("Percent Wild"), testutil.WithEmail("xabc@corp.test"))
	under := testutil.CreateUser(t, database, testutil.WithName("Under"), testutil.WithEmail("a_b@corp.test"))
	underWild := testutil.CreateUser(t, database, testutil.WithName("Under Wild"), testutil.WithEmail("axb@corp.test"))
	slash := testutil.CreateUser(t, database, testutil.WithName("Slash"), testutil.WithEmail(`\%ab@corp.test`))
	slashWild := testutil.CreateUser(t, database, testutil.WithName("Slash Wild"), testutil.WithEmail("%ab@corp.test"))
	pure := testutil.CreateUser(t, database, testutil.WithName("100% Pure"), testutil.WithEmail("pure@corp.test"))
	club := testutil.CreateUser(t, database, testutil.WithName("1000 Club"), testutil.WithEmail("club@corp.test"))
	me := testutil.CreateUser(t, database, testutil.WithName("Quinn Query"), testutil.WithEmail("quinn@corp.test"))
	client := testutil.LoginAs(t, srv, me)

	cases := []struct {
		q    string
		want []int64
	}{
		{q: "%abc", want: []int64{percent.ID}},
		{q: "a_b@", want: []int64{under.ID}},
		{q: `\%ab`, want: []int64{slash.ID}},
		{q: "100%", want: []int64{pure.ID}},
		{q: "%%%%", want: nil},
	}
	for _, tt := range cases {
		t.Run(tt.q, func(t *testing.T) {
			status, hits := getSearch(t, client, srv.URL, tt.q)
			if status != http.StatusOK {
				t.Fatalf("status %d", status)
			}
			assertHitIDs(t, hits, tt.want)
		})
	}
	if percentWild.ID == 0 || underWild.ID == 0 || slashWild.ID == 0 || club.ID == 0 {
		t.Fatal("fixtures")
	}
}

func TestSearchUsersLimit(t *testing.T) {
	srv := testutil.NewServer(t)
	database := testutil.Database(t, srv)
	aaron := testutil.CreateUser(t, database, testutil.WithName("Aaron Aardvark"), testutil.WithEmail("aaron@corp.test"))
	var created []sqlc.User
	for i := 1; i <= 12; i++ {
		opts := []testutil.UserOption{
			testutil.WithName(fmt.Sprintf("ZedA %02d", i)),
			testutil.WithEmail(fmt.Sprintf("zeda-%02d@corp.test", i)),
		}
		if i == 1 {
			opts = append(opts, testutil.WithAvatarPath("avatar/z01.jpg"))
		}
		created = append(created, testutil.CreateUser(t, database, opts...))
	}
	sameA := testutil.CreateUser(t, database, testutil.WithName("Same Name"), testutil.WithEmail("same-a@corp.test"))
	sameB := testutil.CreateUser(t, database, testutil.WithName("Same Name"), testutil.WithEmail("same-b@corp.test"))
	client := testutil.LoginAs(t, srv, aaron)

	status, hits := getSearch(t, client, srv.URL, "ZedA")
	if status != http.StatusOK || len(hits) != 10 {
		t.Fatalf("status %d len %d", status, len(hits))
	}
	for i, hit := range hits {
		if hitID(t, hit) != created[i].ID {
			t.Fatalf("hit %d id %d want %d (%v)", i, hitID(t, hit), created[i].ID, hit["name"])
		}
	}
	assertHit(t, hits[0], created[0], "/uploads/avatar/z01.jpg")
	for _, hit := range hits[1:] {
		if hit["avatarUrl"] != nil {
			t.Fatalf("avatar %#v", hit["avatarUrl"])
		}
	}

	status, hits = getSearch(t, client, srv.URL, "same")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	assertHitIDs(t, hits, []int64{sameA.ID, sameB.ID})
}

func TestSearchRequiresSession(t *testing.T) {
	srv := testutil.NewServer(t)
	res, err := http.Get(srv.URL + "/api/users/search?q=love")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "unauthenticated" || body["message"] == "" {
		t.Fatalf("body %#v", body)
	}
}

func getSearch(t *testing.T, client *http.Client, base, q string) (int, []map[string]any) {
	t.Helper()
	u, err := url.Parse(base + "/api/users/search")
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("q", q)
	u.RawQuery = query.Encode()
	res, err := client.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	return readHits(t, res)
}

func readHits(t *testing.T, res *http.Response) (int, []map[string]any) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, nil
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var hits []map[string]any
	if err := json.Unmarshal(raw, &hits); err != nil {
		t.Fatalf("body %s: %v", raw, err)
	}
	if hits == nil {
		t.Fatalf("null body %s", raw)
	}
	return res.StatusCode, hits
}

func assertHitIDs(t *testing.T, hits []map[string]any, want []int64) {
	t.Helper()
	if want == nil {
		want = []int64{}
	}
	if len(hits) != len(want) {
		t.Fatalf("got %d hits %#v, want %v", len(hits), hits, want)
	}
	for i, hit := range hits {
		if hitID(t, hit) != want[i] {
			t.Fatalf("hit %d id %d want %d", i, hitID(t, hit), want[i])
		}
		if len(hit) != 4 {
			t.Fatalf("keys %#v", hit)
		}
	}
}

func assertHit(t *testing.T, hit map[string]any, user sqlc.User, avatar any) {
	t.Helper()
	if hitID(t, hit) != user.ID || hit["name"] != user.Name || hit["email"] != user.Email || hit["avatarUrl"] != avatar {
		t.Fatalf("hit %#v want id %d name %s email %s avatar %#v", hit, user.ID, user.Name, user.Email, avatar)
	}
}

func hitID(t *testing.T, hit map[string]any) int64 {
	t.Helper()
	id, ok := hit["id"].(float64)
	if !ok {
		t.Fatalf("id %#v", hit["id"])
	}
	return int64(id)
}
