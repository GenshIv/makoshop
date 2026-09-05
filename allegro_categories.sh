#!/bin/bash

go run ./cmd/allegro-cats -dir ./allegro-saved-pages -out docs/allegro_categories.json

go run ./cmd/allegro-fields -pages allegro-saved-pages -write
