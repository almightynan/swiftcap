CC=g++
CFLAGS=`wx-config --cxxflags`
LDFLAGS=`wx-config --libs`

all: swiftcap_ui

swiftcap_ui: main.o
	$(CC) $(CFLAGS) -o swiftcap_ui main.o $(LDFLAGS)

main.o: main.cpp
	$(CC) $(CFLAGS) -c main.cpp

clean:
	rm -f *.o swiftcap_ui