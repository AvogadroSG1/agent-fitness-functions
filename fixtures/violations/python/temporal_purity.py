"""Temporal purity violation fixture: naive datetime construction."""

from datetime import datetime


def stamp_event(event):
    created = datetime.now()
    event["created"] = created
    return event


def legacy_stamp(event):
    event["updated"] = datetime.utcnow()
    return event
