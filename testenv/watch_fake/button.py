"""The washing machine's start button (washer.py)."""

from __future__ import annotations

from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from . import DOMAIN


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    add([hass.data[DOMAIN]["washer"].button])
