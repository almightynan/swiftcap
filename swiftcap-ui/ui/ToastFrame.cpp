#include "../ui/ToastFrame.h"
#include <wx/filename.h>
#include <wx/stdpaths.h>
#include <wx/artprov.h>
#include <wx/textfile.h>
#include <wx/display.h>
ToastFrame::ToastFrame(wxWindow* parent, const wxString& filePath)
    : wxFrame(parent, wxID_ANY, "", wxDefaultPosition, wxDefaultSize, wxFRAME_NO_TASKBAR | wxSTAY_ON_TOP | wxBORDER_NONE), m_filePath(filePath)
{
    wxFont modernFont(9, wxFONTFAMILY_MODERN, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_NORMAL);
    wxFont modernFontBold(12, wxFONTFAMILY_MODERN, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_BOLD);
    wxPanel* bgPanel = new wxPanel(this, wxID_ANY);
    bgPanel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f,255));
    SetTransparent(255);
    bgPanel->SetMinSize(wxSize(420, 1));
    bgPanel->Bind(wxEVT_PAINT, &ToastFrame::OnPaint, this);
    wxBoxSizer* outerSizer = new wxBoxSizer(wxVERTICAL);
    wxBoxSizer* topSizer = new wxBoxSizer(wxHORIZONTAL);
    wxPanel* closePanel = new wxPanel(bgPanel, wxID_ANY, wxDefaultPosition, wxSize(24,24));
    closePanel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
    closePanel->Bind(wxEVT_PAINT, [](wxPaintEvent& evt){
        wxPaintDC dc(static_cast<wxWindow*>(evt.GetEventObject()));
        dc.SetPen(wxPen(*wxWHITE, 2, wxPENSTYLE_SOLID));
        wxSize sz = dc.GetSize();
        int pad = 7;
        dc.DrawLine(pad, pad, sz.GetWidth()-pad, sz.GetHeight()-pad);
        dc.DrawLine(sz.GetWidth()-pad, pad, pad, sz.GetHeight()-pad);
    });
    closePanel->SetCursor(wxCursor(wxCURSOR_HAND));
    closePanel->Bind(wxEVT_LEFT_DOWN, [this](wxMouseEvent&){ this->Destroy(); });
    closePanel->Bind(wxEVT_ENTER_WINDOW, [closePanel](wxMouseEvent&){ closePanel->SetBackgroundColour(wxColour(30,30,30)); closePanel->Refresh(); });
    closePanel->Bind(wxEVT_LEAVE_WINDOW, [closePanel](wxMouseEvent&){ closePanel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f)); closePanel->Refresh(); });
    topSizer->AddStretchSpacer();
    topSizer->Add(closePanel, 0, wxTOP|wxRIGHT|wxLEFT, 4);
    outerSizer->Add(topSizer, 0, wxEXPAND|wxLEFT|wxRIGHT, 12);
    wxBoxSizer* sizer = new wxBoxSizer(wxVERTICAL);
    wxFileName absFile(filePath);
    wxString absPath = absFile.GetFullPath();
#if defined(wxART_TICK_MARK)
    wxBoxSizer* msgRow = new wxBoxSizer(wxHORIZONTAL);
    wxStaticBitmap* icon = new wxStaticBitmap(bgPanel, wxID_ANY, wxArtProvider::GetBitmap(wxART_TICK_MARK, wxART_OTHER, wxSize(18,18)));
    msgRow->Add(icon, 0, wxALIGN_CENTER_VERTICAL|wxRIGHT, 6);
    wxStaticText* msg = new wxStaticText(bgPanel, wxID_ANY, "Recording saved:");
    msg->SetForegroundColour(*wxWHITE);
    msg->SetFont(modernFontBold);
    msgRow->Add(msg, 0, wxALIGN_CENTER_VERTICAL);
    sizer->Add(msgRow, 0, wxLEFT|wxRIGHT|wxTOP, 20);
#else
    wxStaticText* msg = new wxStaticText(bgPanel, wxID_ANY, "Recording saved:");
    msg->SetForegroundColour(*wxWHITE);
    msg->SetFont(modernFontBold);
    sizer->Add(msg, 0, wxLEFT|wxRIGHT|wxTOP, 20);
#endif
    wxStaticText* pathText = new wxStaticText(bgPanel, wxID_ANY, absPath);
    pathText->SetForegroundColour(wxColour(200,200,255));
    pathText->SetFont(modernFont);
    sizer->Add(pathText, 0, wxLEFT|wxRIGHT|wxBOTTOM, 18);
    wxBoxSizer* btnSizer = new wxBoxSizer(wxHORIZONTAL);
    auto makeBtn = [bgPanel, modernFont](const wxString& label, std::function<void(wxCommandEvent&)> onClick) {
        wxButton* btn = new wxButton(bgPanel, wxID_ANY, label, wxDefaultPosition, wxSize(110,32));
        btn->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
        btn->SetForegroundColour(*wxWHITE);
        btn->SetFont(modernFont);
        btn->SetCursor(wxCursor(wxCURSOR_HAND));
        btn->SetWindowStyleFlag(btn->GetWindowStyleFlag() | wxBORDER_SIMPLE);
        btn->SetMinSize(wxSize(110,32));
        btn->SetMaxSize(wxSize(110,32));
        btn->Bind(wxEVT_BUTTON, onClick);
        btn->Bind(wxEVT_ENTER_WINDOW, [btn](wxMouseEvent&){ btn->SetBackgroundColour(wxColour(40,40,44)); btn->Refresh(); });
        btn->Bind(wxEVT_LEAVE_WINDOW, [btn](wxMouseEvent&){ btn->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f)); btn->Refresh(); });
        return btn;
    };
    btnSizer->Add(makeBtn("Open Folder", [this](wxCommandEvent&){ OnOpenFolder(); }), 0, wxRIGHT, 14);
    btnSizer->Add(makeBtn("Open File", [this](wxCommandEvent&){ OnOpenFile(); }), 0);
    sizer->Add(btnSizer, 0, wxALIGN_RIGHT | wxLEFT|wxRIGHT|wxBOTTOM, 20);
    outerSizer->Add(sizer, 0, wxEXPAND);
    bgPanel->SetSizer(outerSizer);
    bgPanel->SetMinSize(wxSize(420, 1));
    bgPanel->Layout();
    Fit();
    SetMinSize(wxSize(420, bgPanel->GetBestSize().GetHeight()+20));
#if wxUSE_DISPLAY
    wxDisplay display(wxDisplay::GetFromWindow(parent));
    wxRect scr = display.IsOk() ? display.GetClientArea() : wxGetClientDisplayRect();
#else
    int w, h;
    wxGetDisplaySize(&w, &h);
    wxRect scr(0, 0, w, h);
#endif
    Layout();
    wxSize frameSz = GetSize();
    Move(scr.GetRight() - frameSz.GetWidth(), scr.GetBottom() - frameSz.GetHeight());
    Show();
    m_timer = new wxTimer(this);
    Bind(wxEVT_TIMER, &ToastFrame::OnTimer, this);
    m_timer->StartOnce(7000);
}
ToastFrame::~ToastFrame() {
    if (m_timer) {
        m_timer->Stop();
        Unbind(wxEVT_TIMER, &ToastFrame::OnTimer, this);
        delete m_timer;
        m_timer = nullptr;
    }
}
void ToastFrame::OnCloseBtn(wxMouseEvent&) { Close(); }
void ToastFrame::OnPaint(wxPaintEvent& evt) {
    wxPanel* panel = static_cast<wxPanel*>(evt.GetEventObject());
    wxPaintDC dc(panel);
    wxSize sz = panel->GetSize();
    dc.SetPen(wxPen(*wxWHITE, 2));
    dc.SetBrush(*wxTRANSPARENT_BRUSH);
    dc.DrawRoundedRectangle(1, 1, sz.GetWidth()-2, sz.GetHeight()-2, 2);
}
void ToastFrame::OnOpenFolder() {
    wxFileName fn(m_filePath);
    fn.MakeAbsolute();
    wxString folder = fn.GetPath();
    if (folder.IsEmpty() || !wxDirExists(folder)) {
        wxMessageBox("Folder does not exist or path is empty:\n" + folder, "Error", wxICON_ERROR);
        return;
    }
    wxLaunchDefaultApplication(folder);
}
void ToastFrame::OnOpenFile() {
    wxFileName fn(m_filePath);
    fn.MakeAbsolute();
    wxString absFile = fn.GetFullPath();
    if (!wxFileExists(absFile)) {
        wxMessageBox("File does not exist:\n" + absFile, "Error", wxICON_ERROR);
        return;
    }
    wxLaunchDefaultApplication(absFile);
}
void ToastFrame::OnTimer(wxTimerEvent&) {
    Destroy();
}
