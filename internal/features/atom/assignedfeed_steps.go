package atom

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	. "github.com/gucumber/gucumber"
	"github.com/stretchr/testify/assert"
	atomdata "github.com/xtracdev/es-atom-data-pg"
	atompub "github.com/xtracdev/es-atom-pub-pg"
	"github.com/xtracdev/goes"
	"golang.org/x/tools/blog/atom"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"github.com/xtracdev/pgconn"
	"github.com/xtracdev/pgpublish"
)

func init() {

	var atomProcessor *atomdata.AtomDataProcessor
	var initFailed bool
	var feedData, eventData []byte
	var feedID string
	var feed atom.Feed
	var cacheControl string
	var etag string
	var eventID string

	log.Info("Init test envionment")
	config, err := pgconn.NewEnvConfig()
	if err != nil {
		log.Warnf("Failed environment init: %s", err.Error())
		initFailed = true
	}

	db,err := pgconn.OpenAndConnect(config.ConnectString(),1)
	if err != nil {
		log.Warnf("Failed environment init: %s", err.Error())
		initFailed = true
	}

	os.Unsetenv(atompub.KeyAlias)

	Given(`^a single feed with events assigned to it$`, func() {
		log.Info("check init")
		if initFailed {
			assert.False(T, initFailed, "Test env init failure")
			return
		}

		log.Info("Create atom pub processor")
		atomProcessor = atomdata.NewAtomDataProcessor(db.DB)

		log.Info("clean out tables")
		_, err = db.Exec("delete from t_aeae_atom_event")
		assert.Nil(T, err)
		_, err = db.Exec("delete from t_aefd_feed")
		assert.Nil(T, err)

		os.Setenv("FEED_THRESHOLD", "2")
		atomdata.ReadFeedThresholdFromEnv()
		assert.Equal(T, 2, atomdata.FeedThreshold)

		log.Info("add some events")
		eventPtr := &goes.Event{
			Source:   "agg1",
			Version:  1,
			TypeCode: "foo",
			Payload:  []byte("ok"),
		}

		encodedEvent := pgpublish.EncodePGEvent(eventPtr.Source,eventPtr.Version,(eventPtr.Payload).([]byte),eventPtr.TypeCode)
		err = atomProcessor.ProcessMessage(encodedEvent)
		assert.Nil(T, err)

		eventPtr = &goes.Event{
			Source:   "agg2",
			Version:  1,
			TypeCode: "bar",
			Payload:  []byte("ok ok"),
		}

		encodedEvent = pgpublish.EncodePGEvent(eventPtr.Source,eventPtr.Version,(eventPtr.Payload).([]byte),eventPtr.TypeCode)
		err = atomProcessor.ProcessMessage(encodedEvent)
		assert.Nil(T, err)

	})

	When(`^I do a get on the feed resource id$`, func() {
		var err error
		feedID, err = atomdata.RetrieveLastFeed(db.DB)
		assert.Nil(T, err)
		log.Infof("get feed it %s", feedID)

		archiveHandler, err := atompub.NewArchiveHandler(db.DB, "server:12345")
		if !assert.Nil(T, err) {
			return
		}

		router := mux.NewRouter()
		router.HandleFunc(atompub.ArchiveHandlerURI, archiveHandler)

		r, err := http.NewRequest("GET", fmt.Sprintf("/notifications/%s", feedID), nil)
		assert.Nil(T, err)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, r)
		assert.Equal(T, http.StatusOK, w.Result().StatusCode)

		cacheControl = w.Header().Get("Cache-Control")
		etag = w.Header().Get("ETag")

		var readErr error
		feedData, readErr = ioutil.ReadAll(w.Body)
		assert.Nil(T, readErr)

		assert.True(T, len(feedData) > 0, "Empty feed data returned unexpectedly")

	})

	Then(`^all the events associated with the feed are returned$`, func() {
		err = xml.Unmarshal(feedData, &feed)
		if assert.Nil(T, err) {
			assert.Equal(T, 2, len(feed.Entry))
		}

	})

	And(`^there is no previous feed link relationship$`, func() {
		prev := getLink("prev-archive", &feed)
		assert.Nil(T, prev)
	})

	And(`^the next link relationship is recent$`, func() {
		next := getLink("next-archive", &feed)
		if assert.NotNil(T, next) {
			assert.Equal(T, "https://server:12345/notifications/recent", *next)
		}
	})

	And(`^cache headers indicate the resource is cacheable$`, func() {
		if assert.True(T, cacheControl != "") {
			cc := strings.Split(cacheControl, "=")
			if assert.Equal(T, 2, len(cc)) {
				assert.Equal(T, "max-age", cc[0])
				assert.Equal(T, fmt.Sprintf("%d", 30*24*60*60), cc[1])
			}
		}

		assert.Equal(T, feedID, etag)
	})

	Given(`^feedX with prior and next feeds$`, func() {
		log.Info("add 2 more events")
		eventPtr := &goes.Event{
			Source:   "agg3",
			Version:  1,
			TypeCode: "foo",
			Payload:  []byte("ok"),
		}

		encodedEvent := pgpublish.EncodePGEvent(eventPtr.Source,eventPtr.Version,(eventPtr.Payload).([]byte),eventPtr.TypeCode)
		err = atomProcessor.ProcessMessage(encodedEvent)
		assert.Nil(T, err)

		eventPtr = &goes.Event{
			Source:   "agg4",
			Version:  1,
			TypeCode: "bar",
			Payload:  []byte("ok ok"),
		}

		encodedEvent = pgpublish.EncodePGEvent(eventPtr.Source,eventPtr.Version,(eventPtr.Payload).([]byte),eventPtr.TypeCode)
		err = atomProcessor.ProcessMessage(encodedEvent)
		assert.Nil(T, err)

		lastFeed, err := atomdata.RetrieveLastFeed(db.DB)
		assert.Nil(T, err)

		prevOfLast, err := atomdata.RetrievePreviousFeed(db.DB, lastFeed)
		assert.Nil(T, err)

		assert.Equal(T, feedID, prevOfLast.String)

		log.Info("add 2 more events")
		eventPtr = &goes.Event{
			Source:   "agg5",
			Version:  1,
			TypeCode: "foo",
			Payload:  []byte("ok"),
		}

		encodedEvent = pgpublish.EncodePGEvent(eventPtr.Source,eventPtr.Version,(eventPtr.Payload).([]byte),eventPtr.TypeCode)
		err = atomProcessor.ProcessMessage(encodedEvent)
		assert.Nil(T, err)

		eventPtr = &goes.Event{
			Source:   "agg6",
			Version:  1,
			TypeCode: "bar",
			Payload:  []byte("ok ok"),
		}

		encodedEvent = pgpublish.EncodePGEvent(eventPtr.Source,eventPtr.Version,(eventPtr.Payload).([]byte),eventPtr.TypeCode)
		err = atomProcessor.ProcessMessage(encodedEvent)
		assert.Nil(T, err)

		//After this update latest feed will have assigned feed ids for both next
		//and prev. We'll update feed id to this
		feedID = lastFeed
	})

	When(`^I do a get on the feedX resource id$`, func() {
		var err error

		archiveHandler, err := atompub.NewArchiveHandler(db.DB, "server:12345")
		if !assert.Nil(T, err) {
			return
		}

		router := mux.NewRouter()
		router.HandleFunc(atompub.ArchiveHandlerURI, archiveHandler)

		r, err := http.NewRequest("GET", fmt.Sprintf("/notifications/%s", feedID), nil)
		assert.Nil(T, err)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, r)
		assert.Equal(T, http.StatusOK, w.Result().StatusCode)

		cacheControl = w.Header().Get("Cache-Control")
		etag = w.Header().Get("ETag")

		var readErr error
		feedData, readErr = ioutil.ReadAll(w.Body)
		assert.Nil(T, readErr)

		assert.True(T, len(feedData) > 0, "Empty feed data returned unexpectedly")
	})

	Then(`^all the events associated with the updated feed are returned$`, func() {
		//Note that we order the events in the feed by id desc, so agg3 will be the second
		//entry, agg4 will be the first entry.
		feed = atom.Feed{}
		err = xml.Unmarshal(feedData, &feed)
		if assert.Nil(T, err) && assert.Equal(T, 2, len(feed.Entry), "Should be 2 events in the current feed") {
			log.Infof("got %v", feed.Entry)
			assert.Equal(T, fmt.Sprintf("urn:esid:%s:%d", "agg3", 1), feed.Entry[1].ID)
			assert.Equal(T, fmt.Sprintf("urn:esid:%s:%d", "agg4", 1), feed.Entry[0].ID)
			assert.Equal(T, base64.StdEncoding.EncodeToString([]byte("ok")), feed.Entry[1].Content.Body)
		}
	})

	And(`^the previous link relationship refers to the previous feed$`, func() {
		prevfeed, err := atomdata.RetrievePreviousFeed(db.DB, feedID)
		if assert.Nil(T, err) && assert.True(T, prevfeed.Valid) {
			prev := getLink("prev-archive", &feed)
			if assert.NotNil(T, prev) {
				assert.Equal(T, fmt.Sprintf("https://server:12345/notifications/%s", prevfeed.String), *prev)
			}

		}
	})

	And(`^the next link relationship refers to the next feed$`, func() {
		nextfeed, err := atomdata.RetrieveNextFeed(db.DB, feedID)
		if assert.Nil(T, err) && assert.True(T, nextfeed.Valid) {
			next := getLink("next-archive", &feed)
			if assert.NotNil(T, next) {
				assert.Equal(T, fmt.Sprintf("https://server:12345/notifications/%s", nextfeed.String), *next)
			}

		}
	})

	Given(`^an event id exposed via a feed$`, func() {
		if assert.True(T, len(feed.Entry) > 1) {
			eventID = feed.Entry[1].ID
		}
	})

	When(`^I retrieve the event by its id$`, func() {
		var err error

		eventHandler, err := atompub.NewEventRetrieveHandler(db.DB)
		if !assert.Nil(T, err) {
			return
		}

		eventIDParts := strings.Split(eventID, ":")

		router := mux.NewRouter()
		router.HandleFunc(atompub.RetrieveEventHanderURI, eventHandler)

		eventResource := fmt.Sprintf("/events/%s/%s", eventIDParts[2], eventIDParts[3])
		log.Infof("Retrieve event via %s", eventResource)
		r, err := http.NewRequest("GET", eventResource, nil)
		assert.Nil(T, err)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, r)
		assert.Equal(T, http.StatusOK, w.Result().StatusCode)

		cacheControl = w.Header().Get("Cache-Control")
		etag = w.Header().Get("ETag")

		var readErr error
		eventData, readErr = ioutil.ReadAll(w.Body)
		assert.Nil(T, readErr)

		assert.True(T, len(eventData) > 0, "Empty feed data returned unexpectedly")
	})

	Then(`^the event detail is returned$`, func() {
		var event atompub.EventStoreContent
		err := xml.Unmarshal(eventData, &event)
		if assert.Nil(T, err) {
			log.Infof("%+v", event)
			assert.Equal(T, base64.StdEncoding.EncodeToString([]byte("ok")), event.Content)
		}
	})

	And(`^cache headers for the event indicate the resource is cacheable$`, func() {
		if assert.True(T, cacheControl != "") {
			cc := strings.Split(cacheControl, "=")
			if assert.Equal(T, 2, len(cc)) {
				assert.Equal(T, "max-age", cc[0])
				assert.Equal(T, fmt.Sprintf("%d", 30*24*60*60), cc[1])
			}
		}

		assert.Equal(T, "agg3:1", etag)
	})

}
