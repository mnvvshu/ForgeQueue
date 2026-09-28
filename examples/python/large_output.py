# examples/python/large_output.py
# Tests the bounded output protection (1MB threshold)
for i in range(25000):
    print(f"Line {i:06d}: A" + ("X" * 100))
