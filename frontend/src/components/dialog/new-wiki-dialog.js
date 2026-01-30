import React from 'react';
import PropTypes from 'prop-types';
import { gettext } from '../../utils/constants';
import { Button, Input } from 'reactstrap';

const propTypes = {
  toggleCancel: PropTypes.func.isRequired,
  addWiki: PropTypes.func.isRequired,
};

class NewWikiDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isExist: false,
      name: '',
      repoID: '',
      isSubmitBtnActive: false,
    };
  }

  inputNewName = (e) => {
    if (!event.target.value.trim()) {
      this.setState({isSubmitBtnActive: false});
    } else {
      this.setState({isSubmitBtnActive: true});
    }

    this.setState({
      name: e.target.value,
    });
  };

  handleKeyDown = (e) => {
    if (e.key === 'Enter') {
      this.handleSubmit();
    }
  };

  handleSubmit = () => {
    let { isExist, name, repoID } = this.state;
    this.props.addWiki(isExist, name, repoID);
    this.props.toggleCancel();
  };

  toggle = () => {
    this.props.toggleCancel();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('New Wiki')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <label className="form-label">{gettext('Name')}</label>
          <Input onKeyDown={this.handleKeyDown} autoFocus={true} value={this.state.name} onChange={this.inputNewName}/>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.handleSubmit} disabled={!this.state.isSubmitBtnActive}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

NewWikiDialog.propTypes = propTypes;

export default NewWikiDialog;
