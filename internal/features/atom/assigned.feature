@assigned
Feature: Read events for a specific feed
  Scenario:
    Given a single feed with events assigned to it
    When I do a get on the feed resource id
    Then all the events associated with the feed are returned
    And there is no previous feed link relationship
    And the next link relationship is recent
    And cache headers indicate the resource is cacheable

  Scenario:
    Given feedX with prior and next feeds
    When I do a get on the feedX resource id
    Then all the events associated with the updated feed are returned
    And the previous link relationship refers to the previous feed
    And the next link relationship refers to the next feed
    And cache headers indicate the resource is cacheable