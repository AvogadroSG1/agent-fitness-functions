import datetime
import decimal
import hashlib
import json
import pathlib
import re


def digest(payload: dict[str, object]) -> str:
    encoded = json.dumps(payload, sort_keys=True).encode()
    today = datetime.date.today().isoformat()
    path = pathlib.Path(today)
    value = decimal.Decimal("1.5")
    marker = re.sub(r"[^0-9]", "", today)
    return hashlib.sha256(encoded + str(path).encode() + str(value).encode() + marker.encode()).hexdigest()
