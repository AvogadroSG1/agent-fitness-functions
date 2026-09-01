"""SQL composition green fixture: parameterized execution."""


def refresh(cursor, event_id):
    cursor.execute("DELETE FROM events WHERE id = %s", (event_id,))
    cursor.executemany("INSERT INTO events (id) VALUES (%s)", [(event_id,)])
