package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

var (
	verbose = flag.Bool("verbose", false, "be verbose")
	web     = flag.String("http", "", "open web browser instead")
)

const (
	statusOK            = "OK"
	statusInformational = "INFORMATIONAL"
	statusUnimplemented = "UNIMPLEMENTED"
	statusNonStrict     = "NON-STRICT"
	statusUnclean       = "UNCLEAN"
	statusFailed        = "FAILED"
)

func failing(behavior string) bool {
	switch behavior {
	case statusUnclean, statusFailed, statusNonStrict:
		return true
	default:
		return false
	}
}

type statusCounter struct {
	Total         int
	OK            int
	Informational int
	Unimplemented int
	NonStrict     int
	Unclean       int
	Failed        int
}

func (c *statusCounter) Inc(s string) {
	c.Total++
	switch s {
	case statusOK:
		c.OK++
	case statusInformational:
		c.Informational++
	case statusNonStrict:
		c.NonStrict++
	case statusUnimplemented:
		c.Unimplemented++
	case statusUnclean:
		c.Unclean++
	case statusFailed:
		c.Failed++
	default:
		panic(fmt.Sprintf("unexpected status %q", s))
	}
}

func main() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatalf("Usage: %s [options] <report-path>", os.Args[0])
	}

	base := path.Dir(flag.Arg(0))

	if addr := *web; addr != "" {
		http.HandleFunc("/", handlerIndex())
		http.Handle("/report/", http.StripPrefix("/report/",
			http.FileServer(http.Dir(base)),
		))
		log.Fatal(http.ListenAndServe(addr, nil))
		return
	}

	var report report
	if err := decodeFile(os.Args[1], &report); err != nil {
		log.Fatal(err)
	}

	servers := make([]string, 0, len(report))
	for s := range report {
		servers = append(servers, s)
	}
	sort.Strings(servers)

	var failed bool
	tw := tabwriter.NewWriter(os.Stderr, 0, 4, 1, ' ', 0)
	for _, server := range servers {
		var (
			srvFailed  bool
			hdrWritten bool
			counter    statusCounter
		)

		var cases []string
		for id := range report[server] {
			cases = append(cases, id)
		}
		sortBySegment(cases)
		for _, id := range cases {
			c := report[server][id]

			var r entryReport
			err := decodeFile(path.Join(base, c.ReportFile), &r)
			if err != nil {
				log.Fatal(err)
			}
			counter.Inc(c.Behavior)
			bad := failing(c.Behavior)
			if bad {
				srvFailed = true
				failed = true
			}
			if *verbose || bad {
				if !hdrWritten {
					hdrWritten = true
					n, _ := fmt.Fprintf(os.Stderr, "AGENT %q\n", server)
					fmt.Fprintf(tw, "%s\n", strings.Repeat("=", n-1))
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", server, id, c.Behavior)
			}
			if bad {
				fmt.Fprintf(tw, "\tdesc:\t%s\n", r.Description)
				fmt.Fprintf(tw, "\texp: \t%s\n", r.Expectation)
				fmt.Fprintf(tw, "\tact: \t%s\n", r.Result)
			}
		}
		if hdrWritten {
			fmt.Fprint(tw, "\n")
		}
		var status string
		if srvFailed {
			status = statusFailed
		} else {
			status = statusOK
		}
		n, _ := fmt.Fprintf(tw, "AGENT %q SUMMARY (%s)\n", server, status)
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", n-1))

		fmt.Fprintf(tw, "TOTAL:\t%d\n", counter.Total)
		fmt.Fprintf(tw, "%s:\t%d\n", statusOK, counter.OK)
		fmt.Fprintf(tw, "%s:\t%d\n", statusInformational, counter.Informational)
		fmt.Fprintf(tw, "%s:\t%d\n", statusUnimplemented, counter.Unimplemented)
		fmt.Fprintf(tw, "%s:\t%d\n", statusNonStrict, counter.NonStrict)
		fmt.Fprintf(tw, "%s:\t%d\n", statusUnclean, counter.Unclean)
		fmt.Fprintf(tw, "%s:\t%d\n", statusFailed, counter.Failed)
		fmt.Fprint(tw, "\n")
		tw.Flush()
	}
	var rc int
	if failed {
		rc = 1
		fmt.Fprintf(tw, "\n\nTEST %s\n\n", statusFailed)
	} else {
		fmt.Fprintf(tw, "\n\nTEST %s\n\n", statusOK)
	}

	tw.Flush()
	os.Exit(rc)
}

type report map[string]server

type server map[string]entry

type entry struct {
	Behavior        string `json:"behavior"`
	BehaviorClose   string `json:"behaviorClose"`
	Duration        int    `json:"duration"`
	RemoveCloseCode int    `json:"removeCloseCode"`
	ReportFile      string `json:"reportFile"`
}

type entryReport struct {
	Description string `json:"description"`
	Expectation string `json:"expectation"`
	Result      string `json:"result"`
	Duration    int    `json:"duration"`
}

func decodeFile(path string, x interface{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	d := json.NewDecoder(f)
	return d.Decode(x)
}

func compareBySegment(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < min(len(as), len(bs)); i++ {
		ax := mustInt(as[i])
		bx := mustInt(bs[i])
		if ax == bx {
			continue
		}
		return ax - bx
	}
	return len(b) - len(a)
}

func mustInt(s string) int {
	const bits = 32 << (^uint(0) >> 63)
	x, err := strconv.ParseInt(s, 10, bits)
	if err != nil {
		panic(err)
	}
	return int(x)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func handlerIndex() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if *verbose {
			log.Printf("reqeust to %s", r.URL)
		}
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := index.Execute(w, nil); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Fatal(err)
			return
		}
	}
}

var index = template.Must(template.New("").Parse(`
<html>
<body>
<h1>Welcome to WebSocket test server!</h1>
<h4>Ready to Autobahn!</h4>
<a href="/report">Reports</a>
</body>
</html>
`))
