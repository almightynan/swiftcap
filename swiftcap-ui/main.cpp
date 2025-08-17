/*
    you may or may not ask: why did i write UI in C++?
    > to piss off the react devs, that's the aim. [https://youtu.be/watch?v=SRgLA8X5N_4]
*/

#include <wx/wx.h>
#include "recording/MyFrame.h"

class MyApp : public wxApp {
public:
    virtual bool OnInit();
};

wxIMPLEMENT_APP(MyApp);

bool MyApp::OnInit() {
    MyFrame *frame = new MyFrame("SwiftCap");
    frame->Show(true);
    return true;
}