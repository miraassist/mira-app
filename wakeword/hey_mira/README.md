# "Hey Mira" wake word

A [microWakeWord](https://github.com/kahrendt/microWakeWord) model for "Hey Mira", for Echo devices running
[TECHO5](https://github.com/HuskerMinion/techo5). Two files belong together: `hey_mira.json` and `hey_mira.tflite`.

Trained on German speech: about 20,000 synthetic samples of "Hey Mira" and "Hey, Mira" from German Piper voices
(236 speakers and eight single voices, at several speeds), against negative data of speech, a dinner party's noise
and silence. The model's own threshold is 0.95.

## Installing it on a TECHO5 device

**On the device's setup page** (the quickest way):

1. Open the device's setup page, **Voice**, **Wake words of your own**.
2. Choose `hey_mira.json` and `hey_mira.tflite`, then **Add wake word**. The device checks that it can run the model.
3. Pick **Hey Mira** under **Wake word**. Home Assistant takes it over within seconds, once the device is idle.

**Or through Home Assistant:**

1. Put both files into the folder `custom_wake_words` in Home Assistant's config folder (next to
   `configuration.yaml`). Make the folder if it is not there.
2. Restart Home Assistant: it reads the folder only at its start.
3. On the device's Assist satellite, pick **Hey Mira** in the **Wake word** list. The device downloads it and keeps it.

## If it wakes too often or too rarely

The device's **Wake word threshold** sets how sure it has to be: higher wakes by mistake less often, lower hears you
more easily. Say "Hey Mira" a few times from where you will use it and watch **Last wake score** on the device in
Home Assistant (or the scores on the setup page); raise the threshold a step at a time if it wakes by mistake.

More in TECHO5's guide: [A wake word of your own](https://github.com/HuskerMinion/techo5/blob/main/docs/setup.md#a-wake-word-of-your-own).
