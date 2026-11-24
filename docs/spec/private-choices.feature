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

  @step6
  Scenario: PC-12 The target names its buttons in the person's language
    Given a target with labels in de and en: "Vorschlag {n} senden", "Was anderes senden", "Ignorieren"
    When Anna, who speaks German, is asked privately with three proposals
    Then Telegram shows one row each: "Vorschlag 1 senden" to "Vorschlag 3 senden", "Was anderes senden", "Ignorieren"
    And the app shows "Vorschlag 1 senden", "Was anderes senden" with a text field, "Ignorieren"
    And a target without labels keeps "Ja", "Nein" and "Vorschlag {n}"

  @step6
  Scenario: PC-13 A tap on "Was anderes senden" asks what to send instead
    When Anna taps "Was anderes senden" in Telegram
    Then the buttons go, the question "Was soll ich stattdessen antworten?" goes into her chat and into the thread's log
    And the thread stays open

  @step6
  Scenario: PC-14 New proposals are asked again privately
    Given Anna's thread waits for a choice and asks privately
    When a turn writes new proposals into it
    Then the new proposals go to her privately with their buttons

  @step6
  Scenario: PC-15 A chosen proposal is the yes
    When Anna taps "Vorschlag 2 senden", or a turn fills the chosen field with the second proposal
    Then the thread finishes with it, and no private yes/no question is left for it

  @step6
  Scenario: PC-16 "Ignorieren" ends a thread that waits for a choice
    When Anna taps "Ignorieren" while her thread waits for a choice
    Then the thread ends as declined

  @step6
  Scenario: PC-17 A label for a proposal without its number is a problem of the skill
    Given a target whose labels name choose without {n}
    Then the skill has a problem naming the label

  @step6
  Scenario: PC-18 A button of replaced proposals does nothing
    Given Anna got proposals with buttons, and then new proposals
    When she taps "Vorschlag 2 senden" on the older message
    Then nothing is chosen, Telegram's answer says it is done already, and a log line names it
