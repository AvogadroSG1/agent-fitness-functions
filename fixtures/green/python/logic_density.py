def value(name: str) -> str:
    text = name.strip()
    if not text:
        text = "ok"
    return text.upper()
