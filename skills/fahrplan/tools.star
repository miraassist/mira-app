# The tool of the skill fahrplan (docs/spec/fahrplan.md, docs/spec/api-tools.md):
# the next departures from dbf.finalrewind.org, as a short list.

_API = "https://dbf.finalrewind.org/"

# The networks dbf knows (its page /_backend, 2026-11-26), by the parameter it
# takes them with.
_EFA = ["AVV", "BEG", "BSVG", "DING", "KVV", "LinzAG", "MVV", "NVBW", "NWL", "RVV", "Rolph", "VAG", "VGN", "VMV",
        "VRN", "VRR", "VRR2", "VRR3", "VVO", "VVS", "bwegt"]
_HAFAS = ["AVV", "BART", "BLS", "BVG", "CFL", "CMTA", "DSB", "IE", "KVB", "NAHSH", "NASA", "NVV", "PKP", "RMV", "RSAG",
          "Resrobot", "STV", "SaarVV", "TPG", "VBB", "VBN", "VMT", "VOS", "ZVV", "mobiliteit"]

# dbf's answers for a stop it does not find (decision 3).
_NICHT_EINDEUTIG = ["name list", "Ambiguous station name"]
_UNBEKANNT = ["stop invalid", "LOCATION"]

_MAX = 20
_ZONE = "Europe/Berlin"

def abfahrten(haltestelle = "", richtung = ""):
    """Die nächsten Abfahrten an einer Haltestelle: Art, Linie, Ziel, Minuten bis zur Abfahrt, Uhrzeit, Verspätung, Ausfall und Steig, bei Zügen auch Halte unterwegs und Gründe für Verspätungen. Höchstens 20.

    Kostet eine Abfrage bei dbf.finalrewind.org.

    Args:
      haltestelle (str): Eine andere Haltestelle als die des Hauses, so geschrieben, wie der Verbund sie schreibt. Leer: die des Hauses.
      richtung (str): Nur Abfahrten, deren Ziel oder Halte unterwegs das enthalten (z. B. Stuttgart, ZOB). Leer: alle.
    """
    name = haltestelle.strip() or settings.get("haltestelle", "").strip()
    if not name:
        fail("Es ist keine Haltestelle eingestellt: Trag sie in der Einstellung haltestelle des Skills fahrplan ein.")
    param, netz = _netz(settings.get("verbund", ""))
    params = {"limit": 30}
    if param:
        params[param] = netz
    r = api.get(_API + _enc(name) + ".json", params = params)
    j = r.json if type(r.json) == "dict" else None
    fehler = j.get("error", "") if j else ""
    if type(fehler) != "string":
        fehler = ""
    for f in _NICHT_EINDEUTIG:
        if f in fehler:
            fail(_nicht_gefunden(name, netz, "ist nicht eindeutig"))
    for f in _UNBEKANNT:
        if f in fehler:
            fail(_nicht_gefunden(name, netz, "findet die Fahrplanauskunft nicht"))
    if not r.ok or j == None or type(j.get("departures")) != "list":
        fail("Die Fahrplanauskunft antwortet HTTP %d: %s" % (r.status, r.text[:200]))
    roh = j["departures"]
    if not roh:
        fail(_nicht_gefunden(name, netz, "findet die Fahrplanauskunft nicht"))
    jetzt = time.now().in_location(_ZONE)
    liste = []
    for d in roh:
        if type(d) != "dict":
            print("an entry of dbf's answer is a %s, not an object; left out" % type(d))
            continue
        if param == "efa":
            a = _efa(d, jetzt)
        elif param == "hafas":
            a = _hafas(d, jetzt)
        else:
            a = _bahn(d, jetzt)
        if a == None:
            continue
        if a.get("in_minuten", 0) < 0:
            continue
        liste.append(a)
    if not liste:
        fail("Die Antwort der Fahrplanauskunft lässt sich nicht lesen: %s" % r.text[:200])
    out = {"haltestelle": name, "netz": netz or "Bahn"}
    w = richtung.strip().lower()
    if w:
        liste = [a for a in liste if w in a["ziel"].lower() or any([w in h.lower() for h in a.get("ueber", [])])]
        if not liste:
            out["hinweis"] = "Keine der nächsten Abfahrten fährt Richtung %s." % richtung.strip()
    out["abfahrten"] = liste[:_MAX]
    return out

def _netz(v):
    """The parameter and the network's name as dbf writes it; ("", "") for trains only."""
    v = v.strip()
    if not v:
        return "", ""
    for n in _EFA:
        if n.lower() == v.lower():
            return "efa", n
    for n in _HAFAS:
        if n.lower() == v.lower():
            return "hafas", n
    fail(("Den Verkehrsverbund „%s“ kennt die Fahrplanauskunft nicht. Sie kennt: %s. Für Bremen und Oldenburg ist es " +
          "VBN. Ändere die Einstellung verbund des Skills fahrplan.") % (v, ", ".join(_EFA + [n for n in _HAFAS if n not in _EFA])))

def _nicht_gefunden(name, netz, was):
    wo = " (Verbund %s)" % netz if netz else ""
    return ("Die Haltestelle „%s“%s %s. Der Name muss so geschrieben sein, wie der Verbund ihn schreibt, wie in seiner " +
            "App oder seinem Fahrplan, etwa „Oldenburg(Oldb) Pulverturm“.") % (name, wo, was)

def _enc(s):
    """s for the address's path, every byte but letters, digits and a few marks as %XX."""
    frei = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~(),"
    out = ""
    for b in s.elem_ords():
        c = chr(b)
        if b < 128 and c in frei:
            out += c
        else:
            out += "%" + "0123456789ABCDEF"[b // 16] + "0123456789ABCDEF"[b % 16]
    return out

def _text(x):
    if type(x) == "string":
        return x.strip()
    if type(x) == "int":
        return str(x)
    return ""

def _zahl(x):
    """A number of minutes or seconds as dbf gives it (a number or digits), else None."""
    if type(x) == "int":
        return x
    if type(x) == "float":
        return int(x)
    if type(x) == "string" and x.strip().lstrip("-").isdigit():
        return int(x.strip())
    return None

def _uhr(sekunden):
    return time.from_timestamp(sekunden).in_location(_ZONE).format("15:04")

def _minuten(sekunden, jetzt):
    return int((time.from_timestamp(sekunden) - jetzt).minutes)

def _efa(d, jetzt):
    linie, ziel = _text(d.get("line")), _text(d.get("destination"))
    if not linie or not ziel:
        print("a departure of dbf's answer without line or destination; left out: %s" % json.encode(d)[:200])
        return None
    a = {"art": _text(d.get("type")), "linie": linie, "ziel": ziel}
    plan = _zahl(d.get("sched_datetime")) or _zahl(d.get("datetime"))
    wirklich = _zahl(d.get("rt_datetime")) or _zahl(d.get("datetime")) or plan
    if wirklich != None:
        a["in_minuten"] = _minuten(wirklich, jetzt)
    elif _zahl(d.get("countdown")) != None:
        a["in_minuten"] = _zahl(d.get("countdown"))
    if plan != None:
        a["uhrzeit"] = _uhr(plan)
    _rest(a, _zahl(d.get("delay")), d.get("is_cancelled"), _text(d.get("platform_name")) or _text(d.get("platform")))
    return a

def _hafas(d, jetzt):
    zug, ziel = _text(d.get("train")), _text(d.get("destination"))
    if not zug or not ziel:
        print("a departure of dbf's answer without train or destination; left out: %s" % json.encode(d)[:200])
        return None
    a = {"art": zug.split(" ")[0], "linie": zug, "ziel": ziel}
    plan = _zahl(d.get("scheduledTime"))
    wirklich = _zahl(d.get("time")) or plan
    if wirklich != None:
        a["in_minuten"] = _minuten(wirklich, jetzt)
    if plan != None:
        a["uhrzeit"] = _uhr(plan)
    _rest(a, _zahl(d.get("delay")), d.get("isCancelled"), _text(d.get("platform")))
    return a

def _bahn(d, jetzt):
    zug, ziel, ab = _text(d.get("train")), _text(d.get("destination")), _text(d.get("scheduledDeparture"))
    if not ab:
        return None  # the train ends here
    if not zug or not ziel or len(ab) != 5 or ab[2] != ":" or not (ab[:2] + ab[3:]).isdigit():
        print("a departure of dbf's answer without train, destination or time; left out: %s" % json.encode(d)[:200])
        return None
    a = {"art": zug.split(" ")[0], "linie": zug, "ziel": ziel, "uhrzeit": ab}
    plan = time.time(year = jetzt.year, month = jetzt.month, day = jetzt.day, hour = int(ab[:2]), minute = int(ab[3:]),
                     location = _ZONE)
    if (plan - jetzt).hours < -12:
        plan = plan + 24 * time.hour
    elif (plan - jetzt).hours > 12:
        plan = plan - 24 * time.hour
    spaet = _zahl(d.get("delayDeparture"))
    a["in_minuten"] = int((plan - jetzt).minutes) + (spaet or 0)
    _rest(a, spaet, d.get("isCancelled"), _text(d.get("platform")))
    via = d.get("via")
    if type(via) == "list":
        ueber = [_text(h) for h in via if _text(h)]
        if ueber:
            a["ueber"] = ueber[:4]
    m = d.get("messages")
    gruende = m.get("delay") if type(m) == "dict" else None
    if type(gruende) == "list":
        texte = [_text(g.get("text")) for g in gruende if type(g) == "dict" and _text(g.get("text"))]
        if texte:
            a["hinweise"] = texte[-2:]
    return a

def _rest(a, spaet, ausfall, gleis):
    if spaet != None:
        a["verspaetung"] = spaet
    if ausfall in [1, True, "1"]:
        a["ausfall"] = True
    if gleis:
        a["gleis"] = gleis
