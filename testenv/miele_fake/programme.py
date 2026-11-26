"""An appliance's programme: each step's watts for its seconds, then 0 W (the same in testenv/miele_fake)."""

from __future__ import annotations

from collections.abc import Callable
from datetime import datetime

from homeassistant.core import CALLBACK_TYPE, HomeAssistant, callback
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.event import async_call_later

AREA = "Keller"


def steps_of(text: str) -> list[tuple[float, float]]:
    """The steps of "300:150, 1200:2000"; empty: none."""
    if not text.strip():
        return []
    return [(float(sec), float(w)) for sec, w in (part.split(":") for part in text.split(","))]


class Programme:
    """Runs steps and tells the watts; ends with 0 W."""

    def __init__(self, hass: HomeAssistant, watts: Callable[[float], None]) -> None:
        self.hass = hass
        self._watts = watts
        self._steps: list[tuple[float, float]] = []
        self._unsub: CALLBACK_TYPE | None = None

    @property
    def running(self) -> bool:
        return self._unsub is not None

    @callback
    def run(self, steps: list[tuple[float, float]], done: Callable[[], None] | None = None) -> None:
        self.cancel()
        self._steps = list(steps)
        self._done = done
        self._next(None)

    @callback
    def _next(self, _now: datetime | None) -> None:
        self._unsub = None
        if not self._steps:
            self._watts(0.0)
            if self._done:
                self._done()
            return
        seconds, watts = self._steps.pop(0)
        self._watts(watts)
        self._unsub = async_call_later(self.hass, seconds, self._next)

    @callback
    def cancel(self) -> None:
        if self._unsub is not None:
            self._unsub()
            self._unsub = None


@callback
def place_device(hass: HomeAssistant, device: dr.DeviceEntry) -> None:
    """Put the device into the Keller unless it has an area."""
    devices = dr.async_get(hass)
    area = ar.async_get(hass).async_get_area_by_name(AREA)
    if device.area_id is None and area is not None:
        devices.async_update_device(device.id, area_id=area.id)
