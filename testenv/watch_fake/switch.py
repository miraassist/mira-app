"""The fake plug of the availability watch (devices.py) and the washing machine's plug (washer.py)."""

from __future__ import annotations

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback, AddEntitiesCallback

from . import DOMAIN
from .devices import entities_of


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    add([hass.data[DOMAIN]["washer"].switch])


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    entities = entities_of("switch")
    hass.data[DOMAIN]["devices"].extend(entities)
    add(entities)
