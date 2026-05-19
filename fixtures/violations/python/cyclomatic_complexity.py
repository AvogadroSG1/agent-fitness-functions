def route_score(kind: str, retries: int, urgent: bool) -> int:
    score = 0
    if kind == "create":
        score += 1
    elif kind == "update":
        score += 1
    elif kind == "delete":
        score += 1
    elif kind == "manual":
        score += 1
    elif kind == "batch":
        score += 1
    elif kind == "sync":
        score += 1
    if retries > 0:
        score += 1
    if retries > 1:
        score += 1
    if retries > 2:
        score += 1
    if urgent:
        score += 1
    return score
