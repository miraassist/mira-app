"""The washing machine on a plug (docs/spec/fake-devices.md, "The washing machine on a plug"), for mira-pro's
skill strom: the plug's switch, the machine's power and its start button. The machine runs a programme through its
steps (seconds and watts each); the plug off holds the rest of it, the plug on again resumes the rest at once. The
programme, the held rest and a loaded programme live in memory, as the dishwasher's programme does."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from homeassistant.components.button import ButtonEntity
from homeassistant.components.sensor import SensorDeviceClass, SensorEntity, SensorStateClass
from homeassistant.components.switch import SwitchEntity
from homeassistant.const import STATE_OFF, UnitOfPower
from homeassistant.core import CALLBACK_TYPE, HomeAssistant, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.event import async_call_later
from homeassistant.helpers.restore_state import RestoreEntity
from homeassistant.util import dt as dt_util

from .house import place

AREA = "Keller"
# An hour: soaking, heating, washing, spinning.
DEFAULT_STEPS = "300:150, 1200:2000, 1500:150, 600:500"


def steps_of(text: str) -> list[tuple[float, float]]:
    """The steps of "300:150, 1200:2000" (validated by the actions' schema)."""
    return [(float(sec), float(w)) for sec, w in (part.split(":") for part in text.split(","))]


class Washer:
    """The machine: its plug, its programme and the rest it holds while the plug is off."""

    def __init__(self, hass: HomeAssistant) -> None:
        self.hass = hass
        self.plug_on = True
        # The programme's rest: the running (or held) step first, with its seconds left.
        self._steps: list[tuple[float, float]] = []
        self._step_started: datetime | None = None
        self._unsub: CALLBACK_TYPE | None = None
        self._loaded: list[tuple[float, float]] | None = None
        self.power = WasherPower(self)
        self.switch = WasherPlug(self)
        self.button = WasherStart(self)

    @callback
    def start(self, steps: list[tuple[float, float]]) -> None:
        """The user presses start at the machine: the programme runs, a running one is replaced."""
        if not self.plug_on:
            raise HomeAssistantError("Fake Waschmaschine has no power: its plug is off, so it does not start")
        self._cancel()
        self._steps = list(steps)
        self._run()

    @callback
    def load(self, steps: list[tuple[float, float]]) -> None:
        """The user loads the machine and picks a programme; Home Assistant may start it with the button."""
        self._loaded = list(steps)

    @callback
    def press(self) -> None:
        """Home Assistant presses start: the loaded programme runs."""
        if self._loaded is None:
            raise HomeAssistantError("Fake Waschmaschine has no programme loaded: load one at the machine first")
        steps = self._loaded
        self.start(steps)
        self._loaded = None

    @callback
    def set_plug(self, on: bool) -> None:
        """The plug switched: off holds the rest of the programme, on resumes it at once."""
        if on == self.plug_on:
            return
        self.plug_on = on
        if not on:
            if self._unsub is not None and self._step_started is not None:
                self._cancel()
                seconds, watts = self._steps[0]
                elapsed = (dt_util.utcnow() - self._step_started).total_seconds()
                self._steps[0] = (max(seconds - elapsed, 0.0), watts)
            self.power.set(0.0)
        elif self._steps:
            self._run()

    @callback
    def _run(self) -> None:
        if not self._steps:
            self.power.set(0.0)
            return
        seconds, watts = self._steps[0]
        self.power.set(watts)
        self._step_started = dt_util.utcnow()
        self._unsub = async_call_later(self.hass, seconds, self._next)

    @callback
    def _next(self, _now: datetime) -> None:
        self._unsub = None
        self._steps.pop(0)
        self._run()

    @callback
    def _cancel(self) -> None:
        if self._unsub is not None:
            self._unsub()
            self._unsub = None

    @callback
    def end(self) -> None:
        """The test HA stops: no timer stays behind."""
        self._cancel()


class WasherPower(SensorEntity):
    """The machine's power: the running step's watts, else 0 W."""

    _attr_name = "Fake Waschmaschine Leistung"
    _attr_unique_id = "watch_fake_washer_power"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_should_poll = False

    def __init__(self, washer: Washer) -> None:
        self._washer = washer
        self._attr_native_value = 0.0

    async def async_added_to_hass(self) -> None:
        place(self.hass, self.entity_id, AREA)

    async def async_will_remove_from_hass(self) -> None:
        self._washer.end()

    @callback
    def set(self, watts: float) -> None:
        if self._attr_native_value != watts:
            self._attr_native_value = watts
            if self.hass is not None:
                self.async_write_ha_state()


class WasherPlug(SwitchEntity, RestoreEntity):
    """The plug's switch; on at first, kept over a restart."""

    _attr_name = "Fake Waschmaschine Steckdose"
    _attr_unique_id = "watch_fake_washer_plug"
    _attr_should_poll = False

    def __init__(self, washer: Washer) -> None:
        self._washer = washer

    @property
    def is_on(self) -> bool:
        return self._washer.plug_on

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if (last := await self.async_get_last_state()) is not None:
            self._washer.plug_on = last.state != STATE_OFF
        place(self.hass, self.entity_id, AREA)

    async def async_turn_on(self, **kwargs: Any) -> None:
        self._washer.set_plug(True)
        self.async_write_ha_state()

    async def async_turn_off(self, **kwargs: Any) -> None:
        self._washer.set_plug(False)
        self.async_write_ha_state()


class WasherStart(ButtonEntity):
    """The machine's start, for Home Assistant: starts the loaded programme."""

    _attr_name = "Fake Waschmaschine Start"
    _attr_unique_id = "watch_fake_washer_start"

    def __init__(self, washer: Washer) -> None:
        self._washer = washer

    async def async_added_to_hass(self) -> None:
        place(self.hass, self.entity_id, AREA)

    async def async_press(self) -> None:
        self._washer.press()
