# examples/python/stdin.py
import sys

data = sys.stdin.read().strip()
print(f"Echo from stdin: {data}")
