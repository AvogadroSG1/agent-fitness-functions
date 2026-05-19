def route_score(kind: str, retries: int, urgent: bool) -> int:
    score = {"create": 1, "update": 1, "delete": 1, "manual": 1, "batch": 1, "sync": 1}.get(kind, 0)
    score += min(max(retries, 0), 3)
    return score + (1 if urgent else 0)
