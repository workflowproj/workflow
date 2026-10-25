// Command fakejira serves the jiratest fixtures as a Jira site, for trying the import wizard
// locally and for the wizard's end-to-end test:
//
//	go run ./internal/jiraimport/jiratest/fakejira -addr 127.0.0.1:8599 -flavor cloud
//
// Point the wizard at http://127.0.0.1:8599 with the printed credentials. The server must be
// allowed to call private addresses (WFM_OUTBOUND_ALLOWED_HOSTS=127.0.0.1, or
// WFM_WEBHOOK_ALLOW_PRIVATE=true in development).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/jiraimport/jiratest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8599", "listen address")
	flavor := flag.String("flavor", "cloud", "cloud or datacenter")
	flag.Parse()
	var site *jiratest.Site
	switch *flavor {
	case "cloud":
		site = jiratest.NewCloud(time.Now().AddDate(0, 0, -20))
		// The screen recording the tests declare but never download (it is over their limit).
		site.Content["40002"] = make([]byte, 3<<20)
		fmt.Printf("Jira Cloud at http://%s\n  email: %s\n  token: %s\n", *addr, site.Email, site.Token)
	case "datacenter":
		site = jiratest.NewDataCenter(time.Now().AddDate(0, 0, -30))
		fmt.Printf("Jira Data Center at http://%s\n  personal access token: %s\n", *addr, site.Token)
	default:
		log.Fatalf("unknown flavor %q", *flavor)
	}
	srv := &http.Server{Addr: *addr, Handler: site.Handler("http://" + *addr), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
