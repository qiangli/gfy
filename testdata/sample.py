import os
from pathlib import Path

class Animal:
    def __init__(self, name):
        self.name = name

    def speak(self):
        pass

class Dog(Animal):
    def speak(self):
        return "Woof!"

def create_dog(name):
    return Dog(name)

def instrument():
    tracer.start_span("operation")
    Counter("requests_total")
