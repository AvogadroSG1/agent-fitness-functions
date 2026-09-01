"""SQL composition violation fixture: interpolated statements to execute."""


def refresh(cursor, table):
    cursor.execute(f"DELETE FROM {table}")
    cursor.execute("INSERT INTO %s VALUES (1)" % table)
    cursor.executemany("UPDATE {} SET x = 1".format(table), [()])
