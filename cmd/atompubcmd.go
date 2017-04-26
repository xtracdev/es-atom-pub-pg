package main

import (
	"database/sql"
	"expvar"
	_ "expvar"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	atompub "github.com/xtracdev/es-atom-pub-pg"
	"net/http"
	"os"
	"strings"
	"github.com/xtracdev/pgconn"
)

var insecureConfigBanner = `
 __  .__   __.      _______. _______   ______  __    __  .______       _______
|  | |  \ |  |     /       ||   ____| /      ||  |  |  | |   _  \     |   ____|
|  | |   \|  |    |   (---- |  |__   |  ,----'|  |  |  | |  |_)  |    |  |__
|  | |  .    |     \   \    |   __|  |  |     |  |  |  | |      /     |   __|
|  | |  |\   | .----)   |   |  |____ |   ----.|   --'  | |  |\  \----.|  |____
|__| |__| \__| |_______/    |_______| \______| \______/  | _| '._____||_______|
 `

func init() {
	log.Info("Dumping environment...")
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		if strings.Contains(strings.ToLower(pair[0]), "pass") {
			log.Info(fmt.Sprintf("%s=XXXXXX", pair[0]))
		} else {
			log.Info(e)
		}
	}
}

type atomFeedPubConfig struct {
	linkhost              string
	listenerHostAndPort   string
	hcListenerHostAndPort string
	secure                bool
}

//expvar exports on the default service mux, which we are not using here. So the following
//code from expvar.go has been lifter so we can add the expvar GET
func expvarHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, "{\n")
	first := true
	expvar.Do(func(kv expvar.KeyValue) {
		if !first {
			fmt.Fprintf(w, ",\n")
		}
		first = false
		fmt.Fprintf(w, "%q: %s", kv.Key, kv.Value)
	})
	fmt.Fprintf(w, "\n}\n")
}

func newAtomFeedPubConfig() *atomFeedPubConfig {
	var configErr bool
	config := new(atomFeedPubConfig)
	config.linkhost = os.Getenv("LINKHOST")
	if config.linkhost == "" {
		log.Println("Missing LINKHOST environment variable value")
		configErr = true
	}

	config.listenerHostAndPort = os.Getenv("LISTENADDR")
	if config.listenerHostAndPort == "" {
		log.Println("Missing LISTENADDR environment variable value")
		configErr = true
	}

	log.Info("This container exposes its docker health check on port 4567")
	config.hcListenerHostAndPort = ":4567"

	keyAlias := os.Getenv(atompub.KeyAlias)
	if keyAlias == "" {
		log.Println("Missing KEY_ALIAS environment variable value - required for secure config")
		log.Println(insecureConfigBanner)
	}

	//Finally, if there were configuration errors, we're finished as we can't start with partial or
	//malformed configuration
	if configErr {
		log.Fatal("Error reading configuration from environment")
	}

	return config
}

func CheckDBConfig(db *sql.DB) error {
	var one int
	return db.QueryRow("select 1").Scan(&one)
}

func makeHealthCheck(db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		wroteHeader := false
		err := CheckDBConfig(db)
		if err != nil {
			wroteHeader = true
			w.WriteHeader(http.StatusInternalServerError)
			log.Warnf("DB error on health check: %s", err.Error())
		}

		err = atompub.CheckKMSConfig()
		if err != nil {
			wroteHeader = true
			w.WriteHeader(http.StatusInternalServerError)
			log.Warnf("Error on KMS config health check: %s", err.Error())
		}

		if wroteHeader == false {
			w.WriteHeader(http.StatusOK)
		}
	}
}

func main() {

	//Read atom pub config
	log.Info("Reading config from the environment")
	feedConfig := newAtomFeedPubConfig()

	log.Info("Connect to DB")
	config, err := pgconn.NewEnvConfig()
	if err != nil {
		log.Fatalf("Failed environment init: %s", err.Error())
	}

	postgressConnection,err := pgconn.OpenAndConnect(config.ConnectString(),100)
	if err != nil {
		log.Fatalf("Failed environment init: %s", err.Error())
	}

	//Create handlers
	log.Info("Create and register handlers")
	recentHandler, err := atompub.NewRecentHandler(postgressConnection.DB, feedConfig.linkhost)
	if err != nil {
		log.Fatal(err.Error())
	}

	archiveHandler, err := atompub.NewArchiveHandler(postgressConnection.DB, feedConfig.linkhost)
	if err != nil {
		log.Fatal(err.Error())
	}

	retrieveHandler, err := atompub.NewEventRetrieveHandler(postgressConnection.DB)
	if err != nil {
		log.Fatal(err.Error())
	}

	r := mux.NewRouter()
	r.HandleFunc(atompub.RecentHandlerURI, recentHandler)
	r.HandleFunc(atompub.ArchiveHandlerURI, archiveHandler)
	r.HandleFunc(atompub.RetrieveEventHanderURI, retrieveHandler)
	r.HandleFunc(atompub.PingURI, atompub.PingHandler)

	var server *http.Server

	go func() {
		hcMux := http.NewServeMux()
		healthCheck := makeHealthCheck(postgressConnection.DB)
		hcMux.HandleFunc("/health", healthCheck)
		hcMux.HandleFunc("/debug/vars", expvarHandler)
		log.Infof("Health check and expvars listening on %s", feedConfig.hcListenerHostAndPort)
		http.ListenAndServe(feedConfig.hcListenerHostAndPort, hcMux)
	}()

	//Config server
	server = &http.Server{
		Handler: r,
		Addr:    feedConfig.listenerHostAndPort,
	}

	//Listen up...
	log.Info("Start server")
	log.Fatal(server.ListenAndServe())
}
