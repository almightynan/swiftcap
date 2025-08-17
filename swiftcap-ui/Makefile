CC=g++
CFLAGS=`wx-config --cxxflags`
LDFLAGS=`wx-config --libs`

swiftcap_ui: main.o

OBJS = main.o ui/ModernButton.o ui/ToastFrame.o recording/MyFrame.o

all: swiftcap_ui

swiftcap_ui: $(OBJS)
	$(CC) $(CFLAGS) -o swiftcap_ui $(OBJS) $(LDFLAGS)

main.o: main.cpp
	$(CC) $(CFLAGS) -Iui -Irecording -c main.cpp

ui/ModernButton.o: ui/ModernButton.cpp ui/ModernButton.h
	$(CC) $(CFLAGS) -Iui -Irecording -c ui/ModernButton.cpp -o ui/ModernButton.o

ui/ToastFrame.o: ui/ToastFrame.cpp ui/ToastFrame.h
	$(CC) $(CFLAGS) -Iui -Irecording -c ui/ToastFrame.cpp -o ui/ToastFrame.o

recording/MyFrame.o: recording/MyFrame.cpp recording/MyFrame.h
	$(CC) $(CFLAGS) -Iui -Irecording -c recording/MyFrame.cpp -o recording/MyFrame.o

clean:
	rm -f *.o swiftcap_ui