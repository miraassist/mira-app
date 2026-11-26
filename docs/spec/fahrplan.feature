# Scenarios of docs/spec/fahrplan.md. Every scenario has a number (FP-01, …); the Go test that checks it carries the
# number in its name (TestFP01_…). @step1 is the tool, @step2 the skill's text and settings.

Feature: FP Departures

  @step1
  Scenario: FP-01 A HAFAS network's departures as a short list
    Given the setting verbund is VBN and dbf answers with HAFAS departures
    When the tool is asked for the house's stop
    Then dbf is asked with hafas=VBN
    And at most 20 departures come back, each with kind, line, destination, minutes, time, delay and platform, without routes

  @step1
  Scenario: FP-02 An EFA network's departures as a short list
    Given the setting verbund is KVV and dbf answers with EFA departures
    When the tool is asked for the house's stop
    Then dbf is asked with efa=KVV
    And the departures come back in the same short form

  @step1
  Scenario: FP-03 Trains without a network, with stops on the way and reasons
    Given the setting verbund is empty and dbf answers with train departures
    When the tool is asked
    Then dbf is asked without efa or hafas
    And each train comes with up to four stops on the way and up to two reasons for a delay, a cancelled one marked

  @step1
  Scenario: FP-04 An unknown network is refused with the known ones
    Given the setting verbund is VWG
    When the tool is asked
    Then it fails with the sentence that the network is unknown, naming the known ones, without asking dbf

  @step1
  Scenario: FP-05 A stop that is not found says how to write it
    Given dbf answers an empty list, "name list", "stop invalid", "LOCATION" or "Ambiguous station name"
    When the tool is asked
    Then it fails with what was asked and that the name must be written as the network writes it

  @step1
  Scenario: FP-06 A direction filters the departures
    Given dbf answers departures to several destinations
    When the tool is asked with a direction
    Then only departures whose destination or stops on the way contain it come back
    And with none left, an empty list with the sentence that none of the next ones goes there

  @step1
  Scenario: FP-07 dbf failing is said, not retried
    Given dbf answers 503, no JSON or nothing within 20 s
    When the tool is asked
    Then it fails with the status and the answer's start, after one request

  @step1
  Scenario: FP-08 A departure that does not read is left out
    Given one departure of dbf's answer has a field of a type Mira does not expect
    When the tool is asked
    Then the others come back and a log line names the one left out

  @step2
  Scenario: FP-09 The skill uses the tool, not web_page
    Given the skill fahrplan is loaded
    Then its text names fahrplan__abfahrten and no address of dbf

  @step2
  Scenario: FP-10 The settings say how to write network and stop
    Given the skill fahrplan is loaded
    Then the setting verbund names examples of networks dbf knows and haltestelle the name as the network writes it
