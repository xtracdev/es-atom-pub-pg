package esatompubpg

import (
	"database/sql"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/xtracdev/envinject"
	atomdata "github.com/xtracdev/es-atom-data-pg"
	"golang.org/x/tools/blog/atom"
)

var ErrBadDBConnection = errors.New("Nil db passed to factory method")
var ErrMissingInjectedEnv = errors.New("Nil env passed to factory method")
var ErrMissingAtomEncrypter = errors.New("Nil atom encrypter passed to factory method")

//URIs assumed by handlers - these are fixed as they embed references relative to the URIs
//used in this package
const (
	PingURI                = "/ping"
	RecentHandlerURI       = "/notifications/recent"
	ArchiveHandlerURI      = "/notifications/{feedId}"
	RetrieveEventHanderURI = "/events/{aggregateId}/{version}"
	LinkProto              = "LINK_PROTO"
)

//Used to serialize event store content when directly retrieving using aggregate id and version
type EventStoreContent struct {
	XMLName     xml.Name  `xml:"http://github.com/xtracdev/goes event"`
	AggregateId string    `xml:"aggregateId"`
	Version     int       `xml:"version"`
	Published   time.Time `xml:"published"`
	TypeCode    string    `xml:"typecode"`
	Content     string    `xml:"content"`
}

//Add the retrieved events for a given feed to the atom feed structure
func addItemsToFeed(feed *atom.Feed, events []atomdata.TimestampedEvent, linkhostport, proto string) {

	for _, event := range events {

		encodedPayload := base64.StdEncoding.EncodeToString(event.Payload.([]byte))

		content := &atom.Text{
			Type: event.TypeCode,
			Body: encodedPayload,
		}

		entry := &atom.Entry{
			Title:     "event",
			ID:        fmt.Sprintf("urn:esid:%s:%d", event.Source, event.Version),
			Published: atom.TimeStr(event.Timestamp.Format(time.RFC3339Nano)),
			Content:   content,
		}

		link := atom.Link{
			Rel:  "self",
			Href: fmt.Sprintf("%s://%s/events/%s/%d", proto, linkhostport, event.Source, event.Version),
		}

		entry.Link = append(entry.Link, link)

		feed.Entry = append(feed.Entry, entry)

	}

}

//NewRecentHandler instantiates the handler for retrieve recent notifications, which are those that have not
//yet been assigned a feed id. This will be served up at /notifications/recent
//The linkhostport argument is used to set the host and port in the link relations URL. This is useful
//when proxying the feed, in which case the link relation URLs can reflect the proxied URLs, not the
//direct URL.
func NewRecentHandler(db *sql.DB, linkhostport string, env *envinject.InjectedEnv, ae *AtomEncrypter) (func(rw http.ResponseWriter, req *http.Request), error) {
	if db == nil {
		return nil, ErrBadDBConnection
	}

	if env == nil {
		return nil, ErrMissingInjectedEnv
	}

	if ae == nil {
		return nil, ErrMissingAtomEncrypter
	}

	linkProto := env.Getenv(LinkProto)
	if linkProto == "" {
		linkProto = "https"
	}

	return func(rw http.ResponseWriter, req *http.Request) {
		events, err := atomdata.RetrieveRecent(db)
		if err != nil {
			log.Warnf("Error retrieving recent items: %s", err.Error())
			http.Error(rw, "Error retrieving feed items", http.StatusInternalServerError)
			return
		}

		latestFeed, err := atomdata.RetrieveLastFeed(db)
		if err != nil {
			log.Warnf("Error retrieving last feed id: %s", err.Error())
			http.Error(rw, "Error retrieving feed id", http.StatusInternalServerError)
			return
		}

		feed := atom.Feed{
			Title:   "Event store feed",
			ID:      "recent",
			Updated: atom.TimeStr(time.Now().Format(time.RFC3339)),
		}

		self := atom.Link{
			Href: fmt.Sprintf("%s://%s/notifications/recent", linkProto, linkhostport),
			Rel:  "self",
		}

		via := atom.Link{
			Href: fmt.Sprintf("%s://%s/notifications/recent", linkProto, linkhostport),
			Rel:  "related",
		}

		feed.Link = append(feed.Link, self)
		feed.Link = append(feed.Link, via)

		if latestFeed != "" {
			previous := atom.Link{
				Href: fmt.Sprintf("%s://%s/notifications/%s", linkProto, linkhostport, latestFeed),
				Rel:  "prev-archive",
			}
			feed.Link = append(feed.Link, previous)
		}

		addItemsToFeed(&feed, events, linkhostport, linkProto)

		out, err := xml.Marshal(&feed)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		encodedOut, err := ae.EncryptOutput(out)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		rw.Header().Add("Cache-Control", "no-store")
		rw.Header().Add("Content-Type", "application/atom+xml")
		rw.Write(encodedOut)
	}, nil
}

//NewArchiveHandler instantiates a handler for retrieving feed archives, which is a set of events
//associated with a specific feed id. This will be served up at /notifications/{feedId}
//The linkhostport argument is used to set the host and port in the link relations URL. This is useful
//when proxying the feed, in which case the link relation URLs can reflect the proxied URLs, not the
//direct URL.
func NewArchiveHandler(db *sql.DB, linkhostport string, env *envinject.InjectedEnv, ae *AtomEncrypter) (func(rw http.ResponseWriter, req *http.Request), error) {
	if db == nil {
		return nil, ErrBadDBConnection
	}

	if env == nil {
		return nil, ErrMissingInjectedEnv
	}

	if ae == nil {
		return nil, ErrMissingAtomEncrypter
	}

	linkProto := env.Getenv(LinkProto)
	if linkProto == "" {
		linkProto = "https"
	}

	return func(rw http.ResponseWriter, req *http.Request) {
		feedID := mux.Vars(req)["feedId"]
		if feedID == "" {
			http.Error(rw, "No feed id in uri", http.StatusBadRequest)
			return
		}

		log.Infof("processing request for feed %s", feedID)

		//Retrieve events for the given feed id.
		latestFeed, err := atomdata.RetrieveArchive(db, feedID)
		if err != nil {
			log.Warnf("Error retrieving last feed id: %s", err.Error())
			http.Error(rw, "Error retrieving feed id", http.StatusInternalServerError)
			return
		}

		//Did we get any events? We should not have a feed other than recent with no events, therefore
		//if there are no events then the feed id does not exist.
		if len(latestFeed) == 0 {
			log.Infof("No data found for feed %s", feedID)
			http.Error(rw, "", http.StatusNotFound)
			return
		}

		previousFeed, err := atomdata.RetrievePreviousFeed(db, feedID)
		if err != nil {
			log.Warnf("Error retrieving previous feed id: %s", err.Error())
			http.Error(rw, "Error retrieving previous feed id", http.StatusInternalServerError)
			return
		}

		nextFeed, err := atomdata.RetrieveNextFeed(db, feedID)
		if err != nil {
			log.Warnf("Error retrieving next feed id: %s", err.Error())
			http.Error(rw, "Error retrieving next feed id", http.StatusInternalServerError)
			return
		}

		feed := atom.Feed{
			Title: "Event store feed",
			ID:    feedID,
		}

		self := atom.Link{
			Href: fmt.Sprintf("%s://%s/notifications/%s", linkProto, linkhostport, feedID),
			Rel:  "self",
		}

		feed.Link = append(feed.Link, self)

		if previousFeed.Valid {
			feed.Link = append(feed.Link, atom.Link{
				Href: fmt.Sprintf("%s://%s/notifications/%s", linkProto, linkhostport, previousFeed.String),
				Rel:  "prev-archive",
			})
		}

		var next string
		if (nextFeed.Valid == true && nextFeed.String == "") || !nextFeed.Valid {
			next = "recent"
		} else {
			next = nextFeed.String
		}

		feed.Link = append(feed.Link, atom.Link{
			Href: fmt.Sprintf("%s://%s/notifications/%s", linkProto, linkhostport, next),
			Rel:  "next-archive",
		})

		addItemsToFeed(&feed, latestFeed, linkhostport, linkProto)

		out, err := xml.Marshal(&feed)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		encodedOut, err := ae.EncryptOutput(out)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		//For all feeds except recent, we can indicate the page can be cached for a long time,
		//e.g. 30 days. The recent page is mutable so we don't indicate caching for it. We could
		//potentially attempt to load it from this method via link traversal.
		if feedID != "recent" {
			log.Infof("setting Cache-Control max-age=2592000 for ETag %s", feedID)
			rw.Header().Add("Cache-Control", "max-age=2592000") //Contents are immutable, cache for a month
			rw.Header().Add("ETag", feedID)
		} else {
			rw.Header().Add("Cache-Control", "no-store")
		}

		rw.Header().Add("Content-Type", "application/atom+xml")
		rw.Write(encodedOut)

	}, nil
}

//NewRetrieveHandler instantiates a handler for the retrieval of specific events by aggregate id
//and version. This will be served at /notifications/{aggregateId}/{version}
func NewEventRetrieveHandler(db *sql.DB, ae *AtomEncrypter) (func(rw http.ResponseWriter, req *http.Request), error) {
	if db == nil {
		return nil, ErrBadDBConnection
	}

	if ae == nil {
		return nil, ErrMissingAtomEncrypter
	}

	return func(rw http.ResponseWriter, req *http.Request) {
		aggregateID := mux.Vars(req)["aggregateId"]
		versionParam := mux.Vars(req)["version"]

		log.Infof("Retrieving event %s %s", aggregateID, versionParam)

		version, err := strconv.Atoi(versionParam)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}

		event, err := atomdata.RetrieveEvent(db, aggregateID, version)
		if err != nil {
			switch err {
			case sql.ErrNoRows:
				http.Error(rw, "", http.StatusNotFound)
			default:
				log.Warnf("Error retrieving event: %s", err.Error())
				http.Error(rw, "Error retrieving event", http.StatusInternalServerError)
			}

			return
		}

		eventContent := EventStoreContent{
			AggregateId: aggregateID,
			Version:     version,
			TypeCode:    event.TypeCode,
			Published:   event.Timestamp,
			Content:     base64.StdEncoding.EncodeToString(event.Payload.([]byte)),
		}

		marshalled, err := xml.Marshal(&eventContent)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		encodedOut, err := ae.EncryptOutput(marshalled)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		rw.Header().Add("Content-Type", "application/xml")
		rw.Header().Add("ETag", fmt.Sprintf("%s:%d", aggregateID, version))
		rw.Header().Add("Cache-Control", "max-age=2592000")

		rw.Write(encodedOut)

	}, nil
}

func PingHandler(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusOK)
}
