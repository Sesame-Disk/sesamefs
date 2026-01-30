import React from 'react';
import PropTypes from 'prop-types';

import { SimpleEditor } from '@seafile/seafile-editor';
import { gettext } from '../../utils/constants';

const propTypes = {
  title: PropTypes.string,
  content: PropTypes.string,
  onCommit: PropTypes.func.isRequired,
  onCloseEditorDialog: PropTypes.func.isRequired,
};

class TermsEditorDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isValueChanged: false,
    };
    this.editorRef = React.createRef();
  }

  static defaultProps = {
    title: gettext('Terms'),
  };

  onKeyDown = (event) => {
    event.stopPropagation();
  };

  toggle = () => {
    const { isValueChanged } = this.state;
    if (isValueChanged) {
      let currentContent = this.getCurrentContent();
      this.props.onCommit(currentContent);
    }
    this.props.onCloseEditorDialog();
  };

  onContentChanged = () => {
    return this.setState({isValueChanged: true});
  };

  getCurrentContent = () => {
    return this.editorRef.current.getValue();
  };

  setSimpleEditorRef = (editor) => {
    this.simpleEditor = editor;
  };

  render() {
    let { content, title } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header conditions-editor-dialog-title">
              <h5 className="modal-title">{title}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body conditions-editor-dialog-main">
          <SimpleEditor
            ref={this.editorRef}
            value={content || ''}
            onContentChanged={this.onContentChanged}
          />
        </div>
      </div>
          </div>
        </div>
    );
  }
}

TermsEditorDialog.propTypes = propTypes;

export default TermsEditorDialog;
