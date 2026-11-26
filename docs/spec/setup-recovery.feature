# Scenarios of docs/spec/setup-recovery.md. Every scenario has a number (SU-01, …); the Go test that checks it carries
# the number in its name (TestSU01_…), the integration's pytest test_su…. @step1 is the service, @step2 the
# integration.

Feature: SU A setup that does not finish

  @step1
  Scenario: SU-01 A limited start that is set up says so in its health
    Given mira.yaml cannot be read
    And Home Assistant's access is set in the setup
    When the service starts
    Then its health says status limited
    And the health carries no error text

  @step1
  Scenario: SU-02 A limited start that is not set up waits for its setup
    Given mira.yaml cannot be read
    And Home Assistant's access is not set
    When the service starts
    Then its health says status setup

  @step1
  Scenario: SU-03 A pairing in the limited start ends in the limited status
    Given mira.yaml cannot be read and the service was never paired
    When Home Assistant pairs it with its address, its token and a model
    Then after starting anew its health says status limited
    And its status, with the token, carries the problem with the file, the line and the error

  @step1
  Scenario: SU-04 The limited start's page says the settings cannot be read
    Given mira.yaml cannot be read
    When the page is opened without a token
    Then it says that Mira's settings cannot be read and that Repairs and the app's log say why
    And it shows no error text
    And while the setup is missing it also shows the setup's steps

  @step2
  Scenario: SU-05 The setup finishes on a limited start
    Given the service answers the pairing and then says status limited
    When "Start using Mira" is submitted
    Then the config entry is made
    And the kept token is forgotten

  @step2
  Scenario: SU-06 The token is kept as soon as the pairing answers
    Given the service answers the pairing and then stays in setup mode
    When "Start using Mira" is submitted and the wait gives up
    Then no config entry is made
    And the token is kept under the service's id
    And the summary says Mira is paired but did not start, that her log says why, and that adding her again needs no code

  @step2
  Scenario: SU-07 A new dialog after a failed setup needs no code
    Given a kept token for the service
    And the service says it is paired
    When Mira is added again
    Then the summary asks for no pairing code
    And "Start using Mira" sends the setup with the kept token and makes the config entry

  @step2
  Scenario: SU-08 Keeping the token fails
    Given Home Assistant's storage refuses the write
    When the pairing answers
    Then Home Assistant's log has a warning with the service's id and without the token
    And the setup goes on

  @step2
  Scenario: SU-09 A pairing that fails keeps nothing
    Given the service refuses the pairing
    When "Start using Mira" is submitted
    Then no token is kept
    And the summary says the pairing did not work
