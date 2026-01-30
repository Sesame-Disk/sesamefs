import React from 'react';
import PropTypes from 'prop-types';

import CreatableSelect from 'react-select/creatable';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import toaster from '../toast';

const propTypes = {
  repoID: PropTypes.string.isRequired,
  refreshTrash: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class CleanTrash extends React.Component {
  constructor(props) {
    super(props);
    this.options = [
      {label: gettext('3 days ago'), value: 3},
      {label: gettext('1 week ago'), value: 7},
      {label: gettext('1 month ago'), value: 30},
      {label: gettext('all'), value: 0}
    ];
    this.state = {
      inputValue: this.options[0],
      submitBtnDisabled: false
    };
  }

  handleInputChange = (value) => {
    this.setState({
      inputValue: value
    });
  };

  formSubmit = () => {
    const inputValue = this.state.inputValue;
    const { repoID } = this.props;

    this.setState({
      submitBtnDisabled: true
    });

    seafileAPI.deleteRepoTrash(repoID, inputValue.value).then((res) => {
      toaster.success(gettext('Clean succeeded.'));
      this.props.refreshTrash();
      this.props.toggleDialog();
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      this.setState({
        formErrorMsg: errorMsg,
        submitBtnDisabled: false
      });
    });
  };

  render() {
    const { formErrorMsg } = this.state;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Clean')}</h5>
              <button type="button" className="close" onClick={this.props.toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <React.Fragment>
            <p>{gettext('Clear files in trash and history：')}</p>
            <CreatableSelect
              defaultValue={this.options[0]}
              options={this.options}
              autoFocus={false}
              onChange={this.handleInputChange}
              placeholder=''
            />
            {formErrorMsg && <p className="error m-0 mt-2">{formErrorMsg}</p>}
          </React.Fragment>
        </div>
        <div className="modal-footer">
          <button className="btn btn-primary" disabled={this.state.submitBtnDisabled} onClick={this.formSubmit}>{gettext('Submit')}</button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

CleanTrash.propTypes = propTypes;

export default CleanTrash;
