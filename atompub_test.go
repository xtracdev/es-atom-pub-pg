package esatompubpg

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"golang.org/x/tools/blog/atom"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	"github.com/xtracdev/envinject"
)

func TestRetrieve(t *testing.T) {

	os.Setenv("STATSD_ENDPOINT", "")
	os.Unsetenv("KEY_ALIAS")
	ts := time.Now()

	var retrieveTests = []struct {
		testName       string
		nilDB          bool
		aggregateId    string
		version        string
		expectedStatus int
		colNames       []string
		rowCols        []driver.Value
		queryError     error
		expectedEvent  *EventStoreContent
	}{
		{
			"retrieve no error",
			false,
			"1234567",
			"1",
			http.StatusOK,
			[]string{"event_time", "typecode", "payload"},
			[]driver.Value{ts, "foo", []byte("yeah ok")},
			nil,
			&EventStoreContent{
				AggregateId: "1234567",
				TypeCode:    "foo",
				Version:     1,
				Content:     base64.StdEncoding.EncodeToString([]byte("yeah ok")),
				Published:   ts,
			},
		},
		{
			"handler with nill db",
			true,
			"1234567",
			"1",
			http.StatusBadRequest,
			[]string{},
			[]driver.Value{},
			nil,
			nil,
		},
		{
			"retrieve with malformed version",
			false,
			"1234567",
			"x",
			http.StatusBadRequest,
			[]string{},
			[]driver.Value{},
			nil,
			nil,
		},
		{
			"retrieve with no rows found",
			false,
			"1234567",
			"1",
			http.StatusNotFound,
			[]string{"event_time", "typecode", "payload"},
			[]driver.Value{},
			nil,
			nil,
		},
		{
			"retrieve with sql error",
			false,
			"1234567",
			"1",
			http.StatusInternalServerError,
			[]string{"event_time", "typecode", "payload"},
			[]driver.Value{},
			errors.New("kaboom"),
			nil,
		},
	}

	for _, test := range retrieveTests {
		t.Run(test.testName, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
			}
			defer db.Close()

			rows := sqlmock.NewRows(test.colNames)
			if len(test.rowCols) > 0 {
				t.Log("add test row data")
				rows = rows.AddRow(test.rowCols...)
			}

			var query *sqlmock.ExpectedQuery
			if len(test.colNames) > 0 {
				query = mock.ExpectQuery("select")
				query = query.WillReturnRows(rows)
			}

			if test.queryError != nil {
				if query == nil {
					query = mock.ExpectQuery("select")
				}
				query = query.WillReturnError(test.queryError)
			}

			var eventHandler func(http.ResponseWriter, *http.Request)
			env,_ := envinject.NewInjectedEnv()
			ae,_ := NewAtomEncrypter(env)
			if test.nilDB == false {
				eventHandler, err = NewEventRetrieveHandler(db,ae)
				assert.Nil(t, err)
			} else {
				eventHandler, err = NewEventRetrieveHandler(nil,ae)
				assert.NotNil(t, err)
				return
			}

			router := mux.NewRouter()
			router.HandleFunc(RetrieveEventHanderURI, eventHandler)

			r, err := http.NewRequest("GET", fmt.Sprintf("/events/%s/%s", test.aggregateId, test.version), nil)
			assert.Nil(t, err)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, r)

			//Validate status code
			assert.Equal(t, test.expectedStatus, w.Result().StatusCode)

			if test.expectedEvent != nil {
				//Read the response
				eventData, err := ioutil.ReadAll(w.Body)
				assert.Nil(t, err)

				var event EventStoreContent
				err = xml.Unmarshal(eventData, &event)
				if assert.Nil(t, err) {
					assert.Equal(t, test.expectedEvent.AggregateId, event.AggregateId)
					assert.Equal(t, test.expectedEvent.TypeCode, event.TypeCode)
					assert.Equal(t, test.expectedEvent.Version, event.Version)
					assert.Equal(t, test.expectedEvent.Content, event.Content)
					assert.True(t, test.expectedEvent.Published.Equal(event.Published))
				}

				//Validate cache headers
				cc := w.Header().Get("Cache-Control")
				assert.Equal(t, "max-age=2592000", cc)

				etag := w.Header().Get("ETag")
				assert.Equal(t, "1234567:1", etag)

				//Validate content type
				assert.Equal(t, "application/xml", w.Header().Get("Content-Type"))
			}

			err = mock.ExpectationsWereMet()
			assert.Nil(t, err)
		})
	}
}

func getLink(linkRelationship string, feed *atom.Feed) *string {
	for _, l := range feed.Link {
		if l.Rel == linkRelationship {
			return &l.Href
		}
	}

	return nil
}

func TestRecentFeedHandler(t *testing.T) {

	addr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}

	log.Infof("addr: %v", ln.LocalAddr())

	os.Setenv("STATSD_ENDPOINT", ln.LocalAddr().String())
	os.Unsetenv("KEY_ALIAS")

	//Run this in the background - end of test will kill it. This is to let us have something to
	//catch any writes of statsd data. Since the data is written using udp we might not see anything
	//show up which is cool - we want to cover the instantiation
	go func() {
		buf := make([]byte, 1024)

		for {
			n, addr, err := ln.ReadFromUDP(buf)
			log.Info("Received ", string(buf[0:n]), " from ", addr)

			if err != nil {
				log.Info("Error: ", err)
			}
		}
	}()

	ts := time.Now()

	var recentTests = []struct {
		testName         string
		nilDB            bool
		expectedStatus   int
		colNamesEvents   []string
		rowColsEvents    []driver.Value
		eventsQueryError error
		colNamesFeed     []string
		rowColsFeed      []driver.Value
		feedQueryErr     error
		expectedPrev     string
		expectedSelf     string
	}{
		{
			"rectrieve recent ok",
			false,
			http.StatusOK,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"feedid"}, []driver.Value{"feed-xxx"}, nil,
			"https://testhost:12345/notifications/feed-xxx",
			"https://testhost:12345/notifications/recent",
		},
		{
			"retrieve recent events query error",
			false,
			http.StatusInternalServerError,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{},
			errors.New("kaboom"),
			[]string{}, []driver.Value{}, nil,
			"", "",
		},
		{
			"retrieve recent feed query error",
			false,
			http.StatusInternalServerError,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"feedid"}, []driver.Value{}, errors.New("kaboom"),
			"", "",
		},
		{
			"retrieve recent nil db error",
			true,
			http.StatusInternalServerError,
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			"", "",
		},
	}

	for _, test := range recentTests {
		t.Run(test.testName, func(t *testing.T) {

			//Create mock db
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
			}
			defer db.Close()

			//Set up rows and query for event data
			eventRows := sqlmock.NewRows(test.colNamesEvents)
			if len(test.rowColsEvents) > 0 {
				eventRows = eventRows.AddRow(test.rowColsEvents...)
			}

			var eventsQuery *sqlmock.ExpectedQuery
			if len(test.colNamesEvents) > 0 {
				eventsQuery = mock.ExpectQuery("select event_time")
				eventsQuery = eventsQuery.WillReturnRows(eventRows)
			}

			if test.eventsQueryError != nil {
				if eventsQuery == nil {
					eventsQuery = mock.ExpectQuery("select event_time")
				}
				eventsQuery = eventsQuery.WillReturnError(test.eventsQueryError)
			}

			//Set up row and query for feed data
			feedRows := sqlmock.NewRows(test.colNamesFeed)
			if len(test.rowColsFeed) > 0 {
				feedRows = feedRows.AddRow(test.rowColsFeed...)
			}

			var feedQuery *sqlmock.ExpectedQuery
			if len(test.colNamesFeed) > 0 {
				feedQuery = mock.ExpectQuery("select feedid")
				feedQuery = feedQuery.WillReturnRows(feedRows)
			}

			if test.feedQueryErr != nil {
				if feedQuery == nil {
					feedQuery = mock.ExpectQuery("select feedid")
				}
				feedQuery = feedQuery.WillReturnError(test.feedQueryErr)
			}

			//Instantiate the handler
			var eventHandler func(http.ResponseWriter, *http.Request)
			env,_ := envinject.NewInjectedEnv()
			ae,_ := NewAtomEncrypter(env)
			if test.nilDB == false {
				eventHandler, err = NewRecentHandler(db, "testhost:12345",env, ae)
				assert.Nil(t, err)
			} else {
				eventHandler, err = NewRecentHandler(nil, "testhost:12345",env, ae)
				assert.NotNil(t, err)
				return
			}

			//Set up the router, route the request
			router := mux.NewRouter()
			router.HandleFunc(RecentHandlerURI, eventHandler)

			r, err := http.NewRequest("GET", RecentHandlerURI, nil)
			assert.Nil(t, err)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, r)

			//Check the status code
			assert.Equal(t, test.expectedStatus, w.Result().StatusCode)

			if test.expectedPrev != "" {
				eventData, err := ioutil.ReadAll(w.Body)
				assert.Nil(t, err)

				var feed atom.Feed
				err = xml.Unmarshal(eventData, &feed)
				if assert.Nil(t, err) {
					assert.Equal(t, "recent", feed.ID)
					_, err := time.Parse(time.RFC3339, string(feed.Updated))
					assert.Nil(t, err)
					prev := getLink("prev-archive", &feed)
					if assert.NotNil(t, prev) {
						assert.Equal(t, test.expectedPrev, *prev)
					}
					self := getLink("self", &feed)
					if assert.NotNil(t, self) {
						assert.Equal(t, test.expectedSelf, *self)
					}

					related := getLink("related", &feed)
					if assert.NotNil(t, related) {
						assert.Equal(t, *self, *related)
					}
				}

				cc := w.Header().Get("Cache-Control")
				assert.Equal(t, "no-store", cc)

				etag := w.Header().Get("ETag")
				assert.Equal(t, "", etag)
			}

			err = mock.ExpectationsWereMet()
			assert.Nil(t, err)
		})
	}
}

func TestRetrieveArchiveHandler(t *testing.T) {
	os.Unsetenv("KEY_ALIAS")
	ts := time.Now()

	var archiveTests = []struct {
		testName         string
		nilDB            bool
		feedid           string
		expectedStatus   int
		colNamesEvents   []string
		rowColsEvents    []driver.Value
		eventsQueryError error
		colNamesPrev     []string
		rowColsPrev      []driver.Value
		feedQueryPrevErr error
		colNamesNext     []string
		rowColsNext      []driver.Value
		feedQueryNextErr error
		expectedPrev     string
		expectedSelf     string
		expectedNext     string
	}{
		{
			"retrieve archive ok",
			false,
			"foo",
			http.StatusOK,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"previous"},
			[]driver.Value{"prev-xxx"},
			nil,
			[]string{"feedid"},
			[]driver.Value{"next-xxx"},
			nil,
			"https://testhost:12345/notifications/prev-xxx",
			"https://testhost:12345/notifications/foo",
			"https://testhost:12345/notifications/next-xxx",
		},
		{
			"retrieve archive recent uri",
			false,
			"recent",
			http.StatusOK,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"previous"},
			[]driver.Value{"prev-xxx"},
			nil,
			[]string{"feedid"},
			[]driver.Value{"next-xxx"},
			nil,
			"https://testhost:12345/notifications/prev-xxx",
			"https://testhost:12345/notifications/recent",
			"https://testhost:12345/notifications/next-xxx",
		},
		{
			"retrieve archive next feed is recent",
			false,
			"foo",
			http.StatusOK,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"previous"},
			[]driver.Value{"prev-xxx"},
			nil,
			[]string{"feedid"},
			[]driver.Value{nil},
			nil,
			"https://testhost:12345/notifications/prev-xxx",
			"https://testhost:12345/notifications/foo",
			"https://testhost:12345/notifications/recent",
		},
		{
			"retrieve archive nil db",
			true,
			"foo",
			http.StatusInternalServerError,
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			"", "", "",
		},
		{
			"retrieve archive not resource",
			false,
			"",
			http.StatusBadRequest,
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			"", "", "",
		},
		{
			"retrieve archive event retrieve error",
			false,
			"foo",
			http.StatusInternalServerError,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{},
			errors.New("boom"),
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			"", "", "",
		},
		{
			"retrieve previous error",
			false,
			"foo",
			http.StatusInternalServerError,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"previous"},
			[]driver.Value{},
			errors.New("boom"),
			[]string{}, []driver.Value{}, nil,
			"", "", "",
		},
		{
			"retrieve next error",
			false,
			"foo",
			http.StatusInternalServerError,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"},
			[]driver.Value{ts, "1x2x333", 3, "foo", []byte("yeah ok")},
			nil,
			[]string{"previous"},
			[]driver.Value{"prev-xxx"},
			nil,
			[]string{"feedid"},
			[]driver.Value{},
			errors.New("boom"),
			"", "", "",
		},
		{
			"retrieve feed id with no data",
			false,
			"foo",
			http.StatusNotFound,
			[]string{"event_time", "aggregate_id", "version", "typecode", "payload"}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			[]string{}, []driver.Value{}, nil,
			"", "", "",
		},
	}

	for _, test := range archiveTests {

		t.Run(test.testName, func(t *testing.T) {
			//Create mock db
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
			}
			defer db.Close()

			//Set up rows and query for event data
			eventRows := sqlmock.NewRows(test.colNamesEvents)
			if len(test.rowColsEvents) > 0 {
				eventRows = eventRows.AddRow(test.rowColsEvents...)
			}

			var eventsQuery *sqlmock.ExpectedQuery
			if len(test.colNamesEvents) > 0 {
				eventsQuery = mock.ExpectQuery("select event_time")
				eventsQuery = eventsQuery.WillReturnRows(eventRows)
			}

			if test.eventsQueryError != nil {
				if eventsQuery == nil {
					eventsQuery = mock.ExpectQuery("select event_time")
				}
				eventsQuery = eventsQuery.WillReturnError(test.eventsQueryError)
			}

			//Set up row and query for prev data
			prevRows := sqlmock.NewRows(test.colNamesPrev)
			if len(test.rowColsPrev) > 0 {
				prevRows = prevRows.AddRow(test.rowColsPrev...)
			}

			var prevQuery *sqlmock.ExpectedQuery
			if len(test.colNamesPrev) > 0 {
				prevQuery = mock.ExpectQuery("select previous")
				prevQuery = prevQuery.WillReturnRows(prevRows)
			}

			if test.feedQueryPrevErr != nil {
				if prevQuery == nil {
					prevQuery = mock.ExpectQuery("select previous")
				}
				prevQuery = prevQuery.WillReturnError(test.feedQueryPrevErr)
			}

			//Set up row and query for next data
			nextRows := sqlmock.NewRows(test.colNamesNext)
			if len(test.rowColsNext) > 0 {
				nextRows = nextRows.AddRow(test.rowColsNext...)
			}

			var nextQuery *sqlmock.ExpectedQuery
			if len(test.colNamesNext) > 0 {
				nextQuery = mock.ExpectQuery("select feedid")
				nextQuery = nextQuery.WillReturnRows(nextRows)
			}

			if test.feedQueryNextErr != nil {
				if nextQuery == nil {
					nextQuery = mock.ExpectQuery("select feedid")
				}
				nextQuery = nextQuery.WillReturnError(test.feedQueryNextErr)
			}

			var archiveHandler func(http.ResponseWriter, *http.Request)
			env,_ := envinject.NewInjectedEnv()
			ae,_ := NewAtomEncrypter(env)
			if test.nilDB == false {
				archiveHandler, err = NewArchiveHandler(db, "testhost:12345", env, ae)
				assert.Nil(t, err)
			} else {
				archiveHandler, err = NewArchiveHandler(nil, "testhost:12345", env, ae)
				assert.NotNil(t, err)
				return
			}

			router := mux.NewRouter()
			if test.feedid == "" {
				//A bit artificial...
				router.HandleFunc("/notifications/", archiveHandler)
			} else {
				router.HandleFunc(ArchiveHandlerURI, archiveHandler)
			}

			var testUri = fmt.Sprintf("/notifications/%s", test.feedid)
			if test.feedid == "" {
				testUri = "/notifications/"
			}
			r, err := http.NewRequest("GET", testUri, nil)
			assert.Nil(t, err)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, r)

			//Check the status code
			assert.Equal(t, test.expectedStatus, w.Result().StatusCode)

			eventData, err := ioutil.ReadAll(w.Body)
			assert.Nil(t, err)

			if test.expectedNext != "" {
				var feed atom.Feed
				err = xml.Unmarshal(eventData, &feed)
				if assert.Nil(t, err) {
					assert.Equal(t, test.feedid, feed.ID)
					assert.Equal(t, "", string(feed.Updated))
					assert.Nil(t, err)
					prev := getLink("prev-archive", &feed)
					if assert.NotNil(t, prev) {
						assert.Equal(t, test.expectedPrev, *prev)
					}
					self := getLink("self", &feed)
					if assert.NotNil(t, self) {
						assert.Equal(t, test.expectedSelf, *self)
					}
					next := getLink("next-archive", &feed)
					if assert.NotNil(t, next) {
						assert.Equal(t, test.expectedNext, *next)
					}

					if assert.Equal(t, 1, len(feed.Entry)) {
						assert.Equal(t, "urn:esid:1x2x333:3", feed.Entry[0].ID)
						assert.Equal(t, "foo", feed.Entry[0].Content.Type)
						_, err = time.Parse(time.RFC3339Nano, string(feed.Entry[0].Published))
						assert.Nil(t, err)
					}

					if feed.ID != "recent" {
						cc := w.Header().Get("Cache-Control")
						assert.Equal(t, "max-age=2592000", cc)

						etag := w.Header().Get("ETag")
						assert.Equal(t, "foo", etag)
					} else {
						cc := w.Header().Get("Cache-Control")
						assert.Equal(t, "no-store", cc)

						etag := w.Header().Get("ETag")
						assert.Equal(t, "", etag)
					}
				}
			}

			err = mock.ExpectationsWereMet()
			assert.Nil(t, err)
		})
	}
}

func TestPingHandler(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc(PingURI, PingHandler)

	r, err := http.NewRequest("GET", PingURI, nil)
	assert.Nil(t, err)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, r)

	//Validate status code
	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
}