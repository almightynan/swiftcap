// ui/ToastFrame.h
#pragma once
#include <wx/wx.h>
class ToastFrame : public wxFrame {
public:
    ToastFrame(wxWindow* parent, const wxString& filePath);
    ~ToastFrame();
private:
    void OnOpenFolder();
    void OnOpenFile();
    void OnPaint(wxPaintEvent&);
    void OnCloseBtn(wxMouseEvent&);
    void OnTimer(wxTimerEvent&);
    wxString m_filePath;
    wxTimer* m_timer = nullptr;
};
