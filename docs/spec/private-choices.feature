# Scenarios of docs/spec/private-choices.md. Every scenario has a number (PC-01, …); the Go test that checks it carries
# the number in its name (TestPC01_…).

Feature: PC Choices in a private question

  @step2
  Scenario: PC-01 A thread waiting for a yes asks privately with buttons
    Given a thread of Anna waiting for her yes is asked over her Telegram chat
    Then the message has the buttons Ja and Nein, their data /mira <id> yes and /mira <id> no

  @step3
  Scenario: PC-02 Proposals become buttons
    Given a thread waiting for a choice among three proposals is asked over Anna's companion app
    Then the notification has the first two proposals and one to answer in words, the text lists all three
    And in Telegram there is a button per proposal and Nein

  @step2
  Scenario: PC-03 A tap on Ja confirms the thread without a model
    When Anna taps Ja in Telegram
    Then the thread is confirmed as by her typed yes, the callback is answered and the buttons are gone

  @step3
  Scenario: PC-04 A tap on a proposal fills the field and confirms
    When Anna taps the second proposal in the app
    Then the thread's field holds the second proposal and the thread is confirmed

  @step2
  Scenario: PC-05 Only the person asked may tap
    When a tap for Anna's question comes from another chat or another Home Assistant user
    Then nothing happens and the log names it

  @step2
  Scenario: PC-06 A late tap does nothing
    Given Anna's thread has ended
    When she taps Ja
    Then nothing happens and Telegram's callback answer says it is done already

  # PC-07 and PC-08 (a tap on a pending confirmation) withdrawn 2026-11-24: a held thing is a thread (decision 8).

  @step5
  Scenario: PC-09 A thread of a target that asks privately proposes over the private channel
    Given a target with private true and Anna's thread of it complete
    Then its proposal goes to Anna as a private message of the thread, with the request and the fields
    And the turn is told that the question went privately, not the proposal

  @step5
  Scenario: PC-10 Without a person the proposal is said in the turn
    Given such a thread without a known person
    Then its proposal is said in the turn as for any thread, and the log names why

  @step5
  Scenario: PC-11 A thread's finish tool says the thread, a model's call does not
    When a thread finishes with its skill's tool
    Then the call's _meta has io.mira-assist/thread with the thread's id
    And a call of a turn has no such field
