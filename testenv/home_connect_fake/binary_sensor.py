"""Home Connect's remote start ("Fernstart", its `remote_start`)."""

from __future__ import annotations

from homeassistant.components.binary_sensor import BinarySensorEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import DOMAIN, HA_ID
from .entity import FakeEntity


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add([RemoteStart(hass.data[DOMAIN])])


class RemoteStart(FakeEntity, BinarySensorEntity):
    _attr_name = "Fernstart"
    _attr_unique_id = f"{HA_ID}-BSH.Common.Status.RemoteControlStartAllowed"

    @property
    def is_on(self) -> bool:
        return self.dryer.remote_start
