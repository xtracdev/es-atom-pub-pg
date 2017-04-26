@recentevents
Feature: Read recent events
  Scenario:
    Given some events not yet assigned to a feed
    And no feeds exist
    When I retrieve the recent resource
    Then the events not yet assigned to a feed are returned
    And there is no previous link relationship
    And there is no next link relationship
    And cache headers indicate the resource is not cacheable

  Scenario:
    Given some more events not yet assigned to a feed
    And previous feeds exist
    When I again retrieve the recent resource
    Then then events not yet assigned to a feed are returned
    And the previous link relationship refers to the most recently created feed
    And there is no next link relationship
    And cache headers indicate the resource is not cacheable