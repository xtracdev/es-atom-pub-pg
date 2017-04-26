package atom

import "golang.org/x/tools/blog/atom"

func getLink(linkRelationship string, feed *atom.Feed) *string {
	for _, l := range feed.Link {
		if l.Rel == linkRelationship {
			return &l.Href
		}
	}

	return nil
}
