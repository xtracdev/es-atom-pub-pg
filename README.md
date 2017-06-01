# Event Store Atom Publisher - Postgres Edition

This project provides an atom feed of published events from the oraeventstore.

Overview

For an event store, this project provides an atom feed of published events. 
The events are organized into feeds based on a number of events per feed, 
as processed by the [es-atom-data-pg](https://github.com/xtracdev/es-atom-data-pg) project.

The most recent events are not associated with a feed, these may be 
retrieved via the /notifications/recent resource. Events associated with 
a feed may be retrieved via the /notifications/{feedid} resource.

Additionally, events may be retrieved individually via /notifications/{aggregate_id}/{version}

Based on semantics associated with event stores (immutable events), 
cache headers are returned for feed pages and entities indicating they 
may be cached for 30 days. The recent page is denoted as uncacheable as 
new events may be added to it up the point it is archived by associating 
the events with a specific feed id.


Dependencies:

<pre>
golang.org/x/tools/blog/atom
github.com/gorilla/mux
</pre>

## Populating Event Store Events

For testing and demo purposes, you can use the following projects to
create event store events that are exposed via this feed:

* [cqrs-sample-pub](https://github.com/xtraclabs/cqrs-sample-pub)
* [es-data-pub-pg](https://github.com/xtracdev/es-data-pub-pg)

First, use genevent.go in the cqrs-sample-pub gen-sample-events directory to create some
events to publish in the ora event store.

Next, use pub.go in the es-data-pub cmd directoy to add the events to
the feed and feed event tables used by this package.

Note that when you run the gucumber tests it will wipe out your events.
You probably don't want to run those against a production event store.

## Encryption

This implementation supports encrypting the content part of the
event using the AWS KMS. Install the [AWS SDK](https://aws.amazon.com/sdk-for-go/) via

<pre>
go get github.com/aws/aws-sdk-go/...
</pre>


## Contributing

To contribute, you must certify you agree with the [Developer Certificate of Origin](http://developercertificate.org/)
by signing your commits via `git -s`. To create a signature, configure your user name and email address in git.
Sign with your real name, do not use pseudonyms or submit anonymous commits.


In terms of workflow:

0. For significant changes or improvement, create an issue before commencing work.
1. Fork the respository, and create a branch for your edits.
2. Add tests that cover your changes, unit tests for smaller changes, acceptance test
for more significant functionality.
3. Run gofmt on each file you change before committing your changes.
4. Run golint on each file you change before committing your changes.
5. Make sure all the tests pass before committing your changes.
6. Commit your changes and issue a pull request.

## License

(c) 2016 Fidelity Investments
Licensed under the Apache License, Version 2.0