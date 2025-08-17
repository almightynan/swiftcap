// ui/ModernButton.h
#pragma once
#include <wx/button.h>
class ModernButton : public wxButton {
public:
    ModernButton(wxWindow* parent, wxWindowID id, const wxString& label, const wxPoint& pos, const wxSize& size, const wxFont& font);
private:
    bool m_hover = false;
    void OnEnter(wxMouseEvent&);
    void OnLeave(wxMouseEvent&);
    void OnUpdateUI(wxUpdateUIEvent&);
    void OnPaint(wxPaintEvent& evt);
};
