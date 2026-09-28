# examples/python/runtime_error.py
def calculate():
    return 100 / 0

print("About to execute invalid math...")
calculate()
