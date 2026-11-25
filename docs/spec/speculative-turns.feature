# Scenarios of docs/spec/speculative-turns.md. Every scenario has a number (SC-01, …); the Go test that checks it
# carries the number in its name (TestSC01_…).

Feature: SC Speculative turns

  @step1
  Scenario: SC-01 Without a setting there is no subscription
    Given mira.yaml has no speculation.events_url
    When the service starts
    Then it subscribes to nothing and turns run as always

  @step1
  Scenario: SC-02 The events of a side-channel turn are read in order
    Given a side channel that sends a recorded stammered turn
    When Mira reads it
    Then she has the turn's start, its two readings, the first one stale, and its final text

  @step1
  Scenario: SC-03 A lost connection is taken up again
    Given Mira is subscribed to the side channel
    When the connection breaks
    Then she logs a warning once and subscribes again after 1 s, then 2 s, up to 60 s

  @step1
  Scenario: SC-04 A URL that is not a side channel ends the subscription
    Given the URL answers 404
    When Mira subscribes
    Then she logs an error naming the URL and does not try again

  @step1
  Scenario: SC-05 A side-channel turn belongs to the one listening satellite
    Given the kitchen satellite is in listening and no other
    When the side channel starts a turn
    Then that turn belongs to the kitchen satellite
    And with two satellites in listening it belongs to none

  @step2
  Scenario: SC-06 A finished-sounding reading starts a run
    Given a side-channel turn at the kitchen satellite
    When a reading comes with complete 0.8
    Then a run starts with its text at the kitchen satellite

  # SC-07 (a reading that sounds unfinished starts nothing) dropped 2026-11-25: every reading starts a run (SC-20).

  @step2
  Scenario: SC-08 A stale reading ends its run
    Given a run on reading 1 asks the model
    When reading 1 goes stale
    Then the run's model call is ended and nothing is kept

  @step2
  Scenario: SC-09 A run stops at a tool that does not only read
    Given a run on "Mach das Licht in der Küche an"
    When the model calls the tool to turn the light on
    Then the tool does not run and the round is kept with its call

  @step2
  Scenario: SC-10 A run runs a read-only tool
    Given a run on "Ist das Licht in der Küche an?"
    When the model calls a tool marked read-only
    Then the tool runs and its result is kept for the next round

  @step2
  Scenario: SC-11 No run while a confirmation waits
    Given the conversation at the kitchen satellite has a confirmation waiting
    When a finished-sounding reading comes
    Then no run starts, the confirmation is still there, and the log line says why

  @step3
  Scenario: SC-12 The same request takes the run's round
    Given a run on "Wie spät ist es?" has its answer
    When Home Assistant calls with "Wie spät ist es?" from the kitchen satellite
    Then the turn asks the model nothing and streams the run's answer

  @step3
  Scenario: SC-13 A different text calls the model
    Given a run on "Schalte das Licht im" has its answer
    When Home Assistant calls with "Schalte das Licht im Wohnzimmer an"
    Then the turn calls the model as always and the run is ended with no_match

  @step3
  Scenario: SC-14 A taken round's writing call runs in the turn
    Given a run stopped at the call to turn the kitchen light on
    When Home Assistant calls with the same text
    Then the turn takes the round, turns the light on once, and asks the model for the next round itself

  @step3
  Scenario: SC-15 The turn waits for a round on its way
    Given a run's first round is still at the model
    When Home Assistant calls with the same text
    Then the turn waits for that round and the model is asked once

  @step3
  Scenario: SC-16 Another person's request is not taken
    Given a run guessed anna at the kitchen satellite
    When the turn's voice names ben
    Then the requests differ and the turn calls the model

  @step3
  Scenario: SC-17 A turn waiting for the model's place ends the run
    Given a model with one place, held by a run
    When a turn at another satellite needs the model
    Then the run is ended and the turn gets the place

  @step2
  Scenario: SC-18 A reading of the wake word's tail starts nothing
    Given a side-channel turn at the kitchen satellite
    When a reading of its first second comes ("irá.")
    Then no run starts on it

  @step3
  Scenario: SC-19 A differing request says where
    Given a run on "Schalte das Licht im"
    When Home Assistant calls with "Schalte das Licht im Wohnzimmer an"
    Then the log says the round, the first message that differs and both texts from where they part

  @step2
  Scenario: SC-20 Every reading starts a run, also one that sounds unfinished
    Given a side-channel turn at the kitchen satellite
    When a reading comes with complete 0.2
    Then a run starts with its text

  @step2
  Scenario: SC-21 The satellite's voice and language are kept across a restart
    Given anna's voice was the last one at the kitchen satellite, and Mira started again
    When a reading comes there
    Then the run is anna's, in the language Home Assistant sent last
